package web

import "embed"

// Content embeds static web assets for the dashboard.
//
//go:embed index.html
var Content embed.FS
