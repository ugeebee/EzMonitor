package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/ugeebee/EzMonitor/backend/internal/config"
	"github.com/ugeebee/EzMonitor/backend/internal/database"
	"github.com/ugeebee/EzMonitor/backend/internal/handlers"
	"github.com/ugeebee/EzMonitor/backend/internal/middleware"
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

	// Handlers
	authHandler := handlers.NewAuthHandler(db.API, cfg.JWTSecret)
	groupHandler := handlers.NewGroupHandler(db.API)
	memberHandler := handlers.NewMemberHandler(db.API)
	ingestHandler := handlers.NewIngestHandler(db.Ingest)

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
		// Health check
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

		// Agent metrics ingestion endpoint
		r.Post("/ingest", ingestHandler.IngestBatch)

		// Protected group & membership routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired(cfg.JWTSecret))

			// Group CRUD & Listing
			r.Post("/groups", groupHandler.CreateGroup)
			r.Get("/groups", groupHandler.ListGroups)

			// Group-specific operations
			r.Route("/groups/{groupId}", func(r chi.Router) {
				// Operations requiring at least group membership
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireGroupMember(db.API))
					r.Get("/", groupHandler.GetGroup)
					r.Get("/members", memberHandler.ListMembers)
				})

				// Request to join (for users not yet in group)
				r.Post("/join", memberHandler.RequestToJoin)

				// Operations requiring Group Owner/Admin (Admitting users, managing roles)
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireGroupAdmin(db.API))

					// Admit a user to the group and assign role
					r.Post("/members", memberHandler.AdmitMember)
					r.Post("/admit", memberHandler.AdmitMember) // Friendly alias

					// Update a member's role (admin, editor, viewer)
					r.Put("/members/{userId}", memberHandler.UpdateMemberRole)

					// Remove a member from the group
					r.Delete("/members/{userId}", memberHandler.RemoveMember)
				})
			})
		})
	})

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

