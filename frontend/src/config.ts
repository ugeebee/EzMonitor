// While Divesh's read API is not wired into the running server, set
// VITE_MOCK_QUERY_API=true in .env.local to render synthetic data in the documented shape.
export const MOCK_QUERY_API = import.meta.env.VITE_MOCK_QUERY_API === 'true'

// Path prefix for Divesh's read routes, in case the team mounts them under something like /read.
export const QUERY_PREFIX: string = import.meta.env.VITE_QUERY_PREFIX ?? ''
