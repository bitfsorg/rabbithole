package daemon

// Web serving enhancements for the BitFS daemon.
//
// This file will contain:
//
// 1. Dashboard route: /_dashboard/*
//    - Serve embedded React SPA from dashboard.FS
//    - Client-side routing fallback (serve index.html for non-file paths)
//    - Strip /_dashboard prefix before serving from embed.FS
//
// 2. Static site hosting (future)
//    - Serve Metanet content as browsable websites
//    - Auto-detect index.html in directories
//    - MIME type detection from file extensions
//
// 3. File browser enhancement (future)
//    - HTML directory listings with navigation
//    - Breadcrumb paths
//    - File size and type display
//
// Implementation notes:
//   - Dashboard uses base path /_dashboard to avoid collision with Metanet content paths
//   - SPA fallback: any path under /_dashboard that doesn't match a static file
//     should serve index.html so React Router handles client-side routing
//   - The dashboard embed.FS is in package dashboard (bitfs/dashboard/embed.go)
//
// TODO: implement registerDashboardRoutes(mux *http.ServeMux)
// TODO: implement serveSPA(w http.ResponseWriter, r *http.Request)
// TODO: implement serveStaticSite(w http.ResponseWriter, r *http.Request, rootNode *NodeInfo)
// TODO: implement enhancedDirectoryListing(w http.ResponseWriter, r *http.Request, node *NodeInfo)
