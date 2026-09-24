package web

import "embed"

// Content embeds static web assets and self-hosted fonts for the dashboard.
//
//go:embed index.html fonts/*
var Content embed.FS
