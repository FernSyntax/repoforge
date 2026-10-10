// Package configs embeds the declarative configuration documents
// shipped with the standard (configs/), so the compiled CLI carries
// the single source of truth with it.
package configs

import "embed"

// GitHub contains the GitHub governance documents
// (github/*.yaml profiles, github/labels.yaml canonical labels, and
// github/rulesets/*.json canonical ruleset payloads).
//
//go:embed github/*.yaml github/rulesets/*.json
var GitHub embed.FS
