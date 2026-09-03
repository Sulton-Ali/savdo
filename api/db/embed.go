// Package db embeds the goose SQL migrations so cmd/savdo can run them
// without needing the source tree on disk at runtime.
package db

import "embed"

// Migrations holds every file under db/migrations, including the
// .gitkeep that currently keeps the empty directory tracked by git.
//
// The pattern is "all:migrations", not "migrations": Go's directory-embed
// rule excludes files whose name starts with "." or "_" unless the "all:"
// prefix is used, and with only .gitkeep present a bare "migrations"
// pattern fails to compile ("contains no embeddable files"). goose ignores
// any non-.sql file it finds, so the .gitkeep is harmless once real
// migrations land.
//
//go:embed all:migrations
var Migrations embed.FS
