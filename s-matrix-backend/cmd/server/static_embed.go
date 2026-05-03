package main

import "embed"

//go:embed dist/* dist/assets/*
var staticFiles embed.FS
