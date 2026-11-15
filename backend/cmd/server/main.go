package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/ugeebee/EzMonitor/backend/internal/auth"
	"github.com/ugeebee/EzMonitor/backend/internal/config"
	"github.com/ugeebee/EzMonitor/backend/internal/database"
	"github.com/ugeebee/EzMonitor/backend/internal/handlers"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
	"github.com/ugeebee/EzMonitor/backend/internal/pairing"
	"github.com/ugeebee/EzMonitor/backend/internal/query"
)

func main() {
	cfg := config.Load()

	log.Println("==================================================")
	log.Println("            EzMonitor Backend Server              ")
	log.Println("==================================================")
	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Listen Address: %s", cfg.ListenAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize database connection pools
	db, err := database.Connect(ctx, cfg.APIDatabaseURL, cfg.IngestDatabaseURL)
	if err != nil {
		log.Fatalf("Database connection initialization failed: %v", err)
	}
	defer db.Close()

	// Pairing hub for WebSocket OTP handshakes
	pairingHub := pairing.NewHub()

	// Handlers
	authHandler := handlers.NewAuthHandler(db.API, cfg.JWTSecret)
	groupHandler := handlers.NewGroupHandler(db.API)
	subgroupHandler := handlers.NewSubgroupHandler(db.API)
	memberHandler := handlers.NewMemberHandler(db.API)
	instanceHandler := handlers.NewInstanceHandler(db.API)
	ingestHandler := handlers.NewIngestHandler(db.Ingest)
	pairingHandler := handlers.NewPairingHandler(db.API, pairingHub)
	ruleHandler := handlers.NewRuleHandler(db.API)
	webhookHandler := handlers.NewWebhookHandler(db.API)

	// Query read API (dashboards, fleet, series, alerts)
	apiDB := stdlib.OpenDBFromPool(db.API)
	queryStore := query.NewStore(apiDB)
	queryHandler := &query.Handler{
		Store: queryStore,
		User: func(r *http.Request) (int64, bool) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				return 0, false
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return 0, false
			}
			claims, err := auth.ValidateToken(parts[1], cfg.JWTSecret)
			if err != nil {
				return 0, false
			}
			return claims.UID, true
		},
	}
	queryMux := http.NewServeMux()
	queryHandler.Register(queryMux)

	// Router setup
	r := chi.NewRouter()

	// Global middlewares
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))

	// CORS configuration for React frontend
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*", "https://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check endpoint
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","time":"%s"}`, time.Now().UTC().Format(time.RFC3339))
	})

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"status":"ok","service":"ezmonitor-backend","time":"%s"}`, time.Now().UTC().Format(time.RFC3339))
		})

		// Public authentication routes
		r.Route("/auth", func(r chi.Router) {
			r.Post("/register", authHandler.Register)
			r.Post("/login", authHandler.Login)

			// Authenticated user profile
			r.Group(func(r chi.Router) {
				r.Use(middleware.AuthRequired(cfg.JWTSecret))
				r.Get("/me", authHandler.Me)
			})
		})

		// Agent metrics ingestion & WebSocket pairing endpoints
		r.Post("/ingest", ingestHandler.IngestBatch)
		r.Get("/agent/pair/ws", pairingHandler.AgentWebSocket)

		// Protected application routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired(cfg.JWTSecret))

			// ==========================================
			// 1. Root Group Management
			// ==========================================
			r.Post("/groups", groupHandler.CreateGroup)
			r.Get("/groups", groupHandler.ListGroups)

			r.Route("/groups/{groupId}", func(r chi.Router) {
				// Accessible to group members
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireGroupMember(db.API))
					r.Get("/", groupHandler.GetGroup)
					r.Get("/subgroups", subgroupHandler.ListSubgroups)
					r.Get("/members", memberHandler.ListMembers)
					r.Get("/rules", ruleHandler.ListRules)
					r.Get("/webhooks", webhookHandler.ListWebhooks)
					r.Put("/alerts/{alertId}/resolve", ruleHandler.ResolveAlert)
					r.Post("/alerts/{alertId}/resolve", ruleHandler.ResolveAlert)
				})

				// Admin-only group operations
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireGroupAdmin(db.API))
					r.Put("/", groupHandler.RenameGroup)
					r.Delete("/", groupHandler.DeleteGroup)
					r.Post("/subgroups", subgroupHandler.CreateSubgroup)
					r.Post("/members", memberHandler.AdmitMember)
					r.Post("/admit", memberHandler.AdmitMember)

					// Alert rules CRUD
					r.Post("/rules", ruleHandler.CreateRule)
					r.Put("/rules/{ruleId}", ruleHandler.UpdateRule)
					r.Delete("/rules/{ruleId}", ruleHandler.DeleteRule)

					// Webhooks CRUD & linking
					r.Post("/webhooks", webhookHandler.CreateWebhook)
					r.Delete("/webhooks/{webId}", webhookHandler.DeleteWebhook)
					r.Post("/rules/{ruleId}/webhooks/{webId}", webhookHandler.LinkRuleWebhook)
					r.Delete("/rules/{ruleId}/webhooks/{webId}", webhookHandler.UnlinkRuleWebhook)
				})
			})

			// Top-level alert resolution
			r.Put("/alerts/{alertId}/resolve", ruleHandler.ResolveAlert)
			r.Post("/alerts/{alertId}/resolve", ruleHandler.ResolveAlert)

			// ==========================================
			// 2. Subgroup Management (RBAC: Admin, Subadmin, Viewer)
			// ==========================================
			r.Route("/subgroups/{subgroupId}", func(r chi.Router) {
				// Read operations (Viewer, Subadmin, Admin)
				// "viewers can only see the metrics"
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireSubgroupAccess(db.API))

					r.Get("/", subgroupHandler.GetSubgroup)
					r.Get("/members", memberHandler.ListMembers)
					r.Get("/instances", instanceHandler.ListInstances)
					r.Get("/instances/{instanceId}/labels", instanceHandler.ListLabels)

					// Viewers inspect time-series metrics
					r.Get("/instances/{instanceId}/metrics", instanceHandler.GetInstanceMetrics)
				})

				// Write / Management operations (Admin and Subadmin ONLY)
				// "admin and subadmin can add instacnes to the specific subgroips viewers can only see the metrics and admin subadmin can rename and add instacne and members"
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireSubgroupAdminOrSubadmin(db.API))

					// Rename subgroup
					r.Put("/", subgroupHandler.RenameSubgroup)

					// Manage instances
					r.Post("/instances", instanceHandler.AddInstance)
					r.Post("/instances/pair", pairingHandler.AdminPairInstance)
					r.Put("/instances/{instanceId}", instanceHandler.RenameInstance)
					r.Delete("/instances/{instanceId}", instanceHandler.DeleteInstance)
					r.Patch("/instances/{instanceId}", instanceHandler.ToggleInstance)
					r.Patch("/instances/{instanceId}/toggle", instanceHandler.ToggleInstance)
					r.Post("/instances/{instanceId}/labels", instanceHandler.AddLabel)
					r.Delete("/instances/{instanceId}/labels/{labelKey}", instanceHandler.DeleteLabel)

					// Manage members
					r.Post("/members", memberHandler.AdmitMember)
					r.Post("/admit", memberHandler.AdmitMember)
					r.Put("/members/{userId}", memberHandler.UpdateMemberRole)
					r.Delete("/members/{userId}", memberHandler.RemoveMember)
				})

				// Delete subgroup (Admin only)
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireGroupAdmin(db.API))
					r.Delete("/", subgroupHandler.DeleteSubgroup)
				})
			})
		})
	})

	// Mount query read API routes (/api/v1/fleet, /api/v1/instances/{id}/series, /api/v1/groups/{id}/alerts, etc.)
	r.NotFound(queryMux.ServeHTTP)

	// Server startup and graceful shutdown
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting HTTP server on %s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-sigChan
	log.Println("\nShutdown signal received. Shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server graceful shutdown error: %v", err)
	} else {
		log.Println("HTTP server stopped successfully.")
	}
}
