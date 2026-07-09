package app

import "embed"

//go:embed templates/* static/*
var configFS embed.FS
