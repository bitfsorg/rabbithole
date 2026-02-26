package dashboard

import "embed"

// FS contains the built dashboard files.
// Uncomment the go:embed directive after running `npm run build` in the dashboard/ directory.
//
// To build: cd dashboard && npm install && npm run build
//
// //go:embed dist/*
var FS embed.FS
