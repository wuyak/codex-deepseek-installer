package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
)

func TestParseArgsAcceptsStatusDoctorRepair(t *testing.T) {
	for _, cmd := range []string{"status", "doctor", "repair"} {
		opts, err := parseArgs([]string{cmd, "--skip-statsig"})
		if err != nil {
			t.Fatalf("parseArgs(%q) failed: %v", cmd, err)
		}
		if opts.command != cmd {
			t.Fatalf("command = %q, want %q", opts.command, cmd)
		}
	}
	if !readOnlyCommand("status") || !readOnlyCommand("doctor") {
		t.Fatal("status/doctor should be read-only commands")
	}
	if !patchCommand("repair") {
		t.Fatal("repair should reuse patch command semantics")
	}
}

func TestStatusReportsNeedRepairWithoutWritingConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	catalogPath := filepath.Join(dir, "models_catalog.json")
	original := "model_provider = \"tokenflux\"\nmodel = \"gpt-5.5\"\n"
	if err := os.WriteFile(configPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	res, changed, err := handleConfig(configPath, catalogPath, "status")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "NEEDS_REPAIR" || !changed {
		t.Fatalf("status result = %+v changed=%v", res, changed)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("status wrote config unexpectedly:\n%s", after)
	}
}

func TestRepairPatchesConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	catalogPath := filepath.Join(dir, "models_catalog.json")
	if err := os.WriteFile(configPath, []byte("model_provider = \"tokenflux\"\nmodel = \"gpt-5.5\"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	res, changed, err := handleConfig(configPath, catalogPath, "repair")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "PATCHED" || !changed {
		t.Fatalf("repair result = %+v changed=%v", res, changed)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "model_catalog_json = "+strconv.Quote(catalogPath)) {
		t.Fatalf("repair did not write catalog path:\n%s", after)
	}
}

func TestPatchModelCatalogJSONLine(t *testing.T) {
	in := "model_provider = \"tokenflux\"\nmodel = \"gpt-5.5\"\n\n[model_providers.tokenflux]\nname = \"tokenflux\"\n"
	out, changed, err := patchModelCatalogJSONLine(in, "/Users/test/.codex/models_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(out, "model_catalog_json = \"/Users/test/.codex/models_catalog.json\"") {
		t.Fatalf("missing catalog line:\n%s", out)
	}
	if strings.Contains(out, "[model_providers.tokenflux]\nmodel_catalog_json") {
		t.Fatalf("catalog line inserted inside provider section:\n%s", out)
	}
}

func TestPatchCatalogUpsertsCompleteDeepSeekObjects(t *testing.T) {
	catalog := map[string]any{
		"models": []any{
			map[string]any{
				"slug":              "gpt-5.5",
				"display_name":      "GPT-5.5",
				"base_instructions": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# Personality\nYou have a vivid inner life as Codex: test.",
				"model_messages": map[string]any{
					"instructions_template": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n{{ personality }}\n\n# General",
					"instructions_variables": map[string]any{
						"personality_friendly": "You have a vivid inner life as Codex: test.",
					},
				},
				"visibility":       "list",
				"supported_in_api": true,
			},
		},
	}
	data, _ := json.Marshal(catalog)
	out, changed, report, err := patchCatalog(data)
	if err != nil {
		t.Fatalf("patchCatalog failed: %v; report=%v", err, report)
	}
	if !changed {
		t.Fatal("expected catalog change")
	}
	var patched map[string]any
	if err := json.Unmarshal(out, &patched); err != nil {
		t.Fatal(err)
	}
	models := patched["models"].([]any)
	if len(models) != 3 {
		t.Fatalf("models len = %d, want 3", len(models))
	}
	found := false
	for _, raw := range models {
		m := raw.(map[string]any)
		if m["slug"] == flashSlug {
			found = true
			if m["visibility"] != "list" || m["supported_in_api"] != true {
				t.Fatalf("bad required fields: %+v", m)
			}
			if !strings.Contains(m["base_instructions"].(string), "You are deepseek-v4-flash, a coding agent.") {
				t.Fatalf("base instructions not transformed: %s", m["base_instructions"])
			}
			mm := m["model_messages"].(map[string]any)
			if !strings.Contains(mm["instructions_template"].(string), "You are deepseek-v4-flash, a coding agent.") {
				t.Fatalf("instructions_template not transformed: %s", mm["instructions_template"])
			}
		}
	}
	if !found {
		t.Fatal("DeepSeek flash object missing")
	}
}

func TestNewDeepSeekCatalogModelHasCodexRequiredFields(t *testing.T) {
	template := map[string]any{
		"slug":              "gpt-5.5",
		"base_instructions": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# General",
		"model_messages": map[string]any{
			"instructions_template": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n{{ personality }}",
			"instructions_variables": map[string]any{
				"personality_friendly": "You have a vivid inner life as Codex: test.",
			},
		},
	}

	model := newDeepSeekCatalogModel(template, deepseekModels[0])

	if model.Slug != flashSlug {
		t.Fatalf("slug = %q, want %q", model.Slug, flashSlug)
	}
	if model.Visibility != "list" || !model.SupportedInAPI {
		t.Fatalf("visibility/API fields not list+true: %+v", model)
	}
	if model.ShellType != "unified_exec" {
		t.Fatalf("shell_type = %q", model.ShellType)
	}
	if model.ApplyPatchToolType != "freeform" {
		t.Fatalf("apply_patch_tool_type = %q", model.ApplyPatchToolType)
	}
	if model.TruncationPolicy.Mode != "tokens" || model.TruncationPolicy.Limit <= 0 {
		t.Fatalf("truncation_policy = %+v", model.TruncationPolicy)
	}
	if model.ContextWindow <= 0 || model.MaxContextWindow <= 0 {
		t.Fatalf("context windows not set: context=%d max=%d", model.ContextWindow, model.MaxContextWindow)
	}
	if model.DefaultReasoningLevel != "high" || len(model.SupportedReasoningLevels) != 2 {
		t.Fatalf("reasoning levels = default %q supported %+v", model.DefaultReasoningLevel, model.SupportedReasoningLevels)
	}
	if model.DefaultReasoningSummary != "auto" || !model.SupportsReasoningSummaries {
		t.Fatalf("reasoning summary fields invalid: default=%q supports=%v", model.DefaultReasoningSummary, model.SupportsReasoningSummaries)
	}
	if len(model.InputModalities) != 1 || model.InputModalities[0] != "text" {
		t.Fatalf("input_modalities = %+v", model.InputModalities)
	}
	if model.SupportsSearchTool {
		t.Fatal("DeepSeek catalog entry should not advertise Codex search tool support")
	}
	if !strings.Contains(model.BaseInstructions, "You are deepseek-v4-flash, a coding agent.") {
		t.Fatalf("base instructions not transformed: %s", model.BaseInstructions)
	}
	templateText, _ := model.ModelMessages["instructions_template"].(string)
	if !strings.Contains(templateText, "You are deepseek-v4-flash, a coding agent.") {
		t.Fatalf("instructions_template not transformed: %s", templateText)
	}
}

func TestPatchCatalogReplacesIncompleteExistingDeepSeekObject(t *testing.T) {
	catalog := map[string]any{
		"models": []any{
			map[string]any{
				"slug":              "gpt-5.5",
				"base_instructions": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# General",
				"model_messages": map[string]any{
					"instructions_template": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.",
				},
			},
			map[string]any{
				"slug": flashSlug,
			},
		},
	}
	data, _ := json.Marshal(catalog)

	out, changed, report, err := patchCatalog(data)
	if err != nil {
		t.Fatalf("patchCatalog failed: %v; report=%v", err, report)
	}
	if !changed {
		t.Fatal("expected incomplete DeepSeek object to be replaced")
	}

	var patched map[string]any
	if err := json.Unmarshal(out, &patched); err != nil {
		t.Fatal(err)
	}
	var flash map[string]any
	for _, raw := range patched["models"].([]any) {
		m := raw.(map[string]any)
		if m["slug"] == flashSlug {
			flash = m
			break
		}
	}
	if flash == nil {
		t.Fatal("flash model missing after patch")
	}
	if flash["visibility"] != "list" || flash["supported_in_api"] != true || flash["apply_patch_tool_type"] != "freeform" {
		t.Fatalf("flash model not completed: %+v", flash)
	}
}

func TestPatchStatsigLevelDB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "leveldb")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := map[string]any{
		"dynamic_configs": map[string]any{
			statsigConfigID: map[string]any{
				"value": map[string]any{
					"available_models": []any{"gpt-5.5"},
				},
			},
		},
	}
	innerData, _ := json.Marshal(inner)
	outerData, _ := json.Marshal(map[string]any{"data": string(innerData)})
	if err := db.Put([]byte("_app://-\x00\x01statsig.cached.evaluations.test"), append([]byte{1}, outerData...), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	t.Setenv("HOME", t.TempDir())
	defer os.Setenv("HOME", oldHome)

	stats, changed, err := inspectOrPatchStatsig(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || !stats.AllPresent {
		t.Fatalf("stats=%+v changed=%v", stats, changed)
	}

	stats, changed, err = inspectOrPatchStatsig(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !stats.AllPresent {
		t.Fatalf("verify stats=%+v changed=%v", stats, changed)
	}
}

func TestPrintableKeyEscapesControlBytes(t *testing.T) {
	got := printableKey([]byte("_app://-\x00\x01statsig.cached.evaluations.test"))
	want := `_app://-\x00\x01statsig.cached.evaluations.test`
	if got != want {
		t.Fatalf("printableKey = %q, want %q", got, want)
	}
}
