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

func completeExistingCatalogModel(slug string) map[string]any {
	return map[string]any{
		"slug":              slug,
		"display_name":      "GPT-5.3-Codex-Spark",
		"base_instructions": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.",
		"model_messages": map[string]any{
			"instructions_template": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.",
		},
		"visibility":       "list",
		"supported_in_api": true,
	}
}

func TestPatchCatalogUpsertsCompleteTokenfluxObjects(t *testing.T) {
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
			completeExistingCatalogModel(sparkSlug),
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
	if len(models) != 5 {
		t.Fatalf("models len = %d, want 5", len(models))
	}
	found := false
	foundHuman := false
	foundSpark := false
	for _, raw := range models {
		m := raw.(map[string]any)
		if m["slug"] == sparkSlug {
			foundSpark = true
			if m["display_name"] != "GPT-5.3-Codex-Spark" {
				t.Fatalf("spark catalog object should be preserved, got %+v", m)
			}
		}
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
		if m["slug"] == humanSlug {
			foundHuman = true
			if m["default_reasoning_level"] != "medium" {
				t.Fatalf("human default reasoning = %q", m["default_reasoning_level"])
			}
			if !strings.Contains(m["base_instructions"].(string), "You are human-llm, a coding agent.") {
				t.Fatalf("human base instructions not transformed: %s", m["base_instructions"])
			}
		}
	}
	if !found {
		t.Fatal("DeepSeek flash object missing")
	}
	if !foundHuman {
		t.Fatal("human-llm object missing")
	}
	if !foundSpark {
		t.Fatal("gpt-5.3-codex-spark object missing")
	}
}

func TestNewTokenfluxCatalogModelHasCodexRequiredFields(t *testing.T) {
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

	model := newTokenfluxCatalogModel(template, tokenfluxModels[0])

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

func TestPatchCatalogReplacesIncompleteExistingTokenfluxObject(t *testing.T) {
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
			completeExistingCatalogModel(sparkSlug),
		},
	}
	data, _ := json.Marshal(catalog)

	out, changed, report, err := patchCatalog(data)
	if err != nil {
		t.Fatalf("patchCatalog failed: %v; report=%v", err, report)
	}
	if !changed {
		t.Fatal("expected incomplete TokenFlux model object to be replaced")
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

func TestPatchCatalogRefreshesExistingTokenfluxObjectWhenTemplateSkillsChange(t *testing.T) {
	oldTemplate := map[string]any{
		"slug":              "gpt-5.4",
		"base_instructions": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# General",
		"model_messages": map[string]any{
			"instructions_template": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.",
		},
		"visibility":       "list",
		"supported_in_api": true,
	}
	oldFlash := buildTokenfluxModel(oldTemplate, tokenfluxModels[0])
	oldPro := buildTokenfluxModel(oldTemplate, tokenfluxModels[1])
	oldHuman := buildTokenfluxModel(oldTemplate, tokenfluxModels[2])
	newTemplate := map[string]any{
		"slug":              "gpt-5.5",
		"base_instructions": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# General",
		"model_messages": map[string]any{
			"instructions_template": "You are Codex, a coding agent based on GPT-5. You and the user share one workspace.\n\n# Skills\n{{ skills_manifest }}",
			"instructions_variables": map[string]any{
				"skills_manifest": "Use local Codex skills when they are provided.",
			},
		},
		"skill_runtime": map[string]any{
			"manifest_version": "2026-05-09",
		},
		"visibility":       "list",
		"supported_in_api": true,
	}
	catalog := map[string]any{
		"models": []any{newTemplate, completeExistingCatalogModel(sparkSlug), oldFlash, oldPro, oldHuman},
	}
	data, _ := json.Marshal(catalog)

	out, changed, report, err := patchCatalog(data)
	if err != nil {
		t.Fatalf("patchCatalog failed: %v; report=%v", err, report)
	}
	if !changed {
		t.Fatal("expected stale TokenFlux model objects to refresh from the latest template")
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
		t.Fatal("flash model missing")
	}
	mm := flash["model_messages"].(map[string]any)
	templateText := mm["instructions_template"].(string)
	if !strings.Contains(templateText, "# Skills") || !strings.Contains(templateText, "{{ skills_manifest }}") {
		t.Fatalf("skills instructions were not refreshed into DeepSeek template: %s", templateText)
	}
	vars := mm["instructions_variables"].(map[string]any)
	if vars["skills_manifest"] != "Use local Codex skills when they are provided." {
		t.Fatalf("skills variables were not preserved: %+v", vars)
	}
	runtime := flash["skill_runtime"].(map[string]any)
	if runtime["manifest_version"] != "2026-05-09" {
		t.Fatalf("template top-level skill metadata was not preserved: %+v", runtime)
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
	evaluationKey := []byte("_app://-\x00\x01statsig.cached.evaluations.test")
	outerData, _ := json.Marshal(map[string]any{"source": "Network", "receivedAt": float64(1), "data": string(innerData)})
	if err := db.Put(evaluationKey, append([]byte{1}, outerData...), nil); err != nil {
		t.Fatal(err)
	}
	lastModifiedKey := []byte("_app://-\x00\x01statsig.last_modified_time.evaluations")
	lastModifiedData, _ := json.Marshal(map[string]any{"evaluations": float64(1)})
	if err := db.Put(lastModifiedKey, append([]byte{1}, lastModifiedData...), nil); err != nil {
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

	db, err = leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawEvaluation, err := db.Get(evaluationKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawLastModified, err := db.Get(lastModifiedKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var outer map[string]any
	if err := json.Unmarshal(rawEvaluation[1:], &outer); err != nil {
		t.Fatal(err)
	}
	if outer["source"] != statsigPinnedSource || outer["receivedAt"] != statsigFutureTimestampMillis {
		t.Fatalf("evaluation was not pinned: %+v", outer)
	}
	var pinnedInner map[string]any
	if err := json.Unmarshal([]byte(outer["data"].(string)), &pinnedInner); err != nil {
		t.Fatal(err)
	}
	config := pinnedInner["dynamic_configs"].(map[string]any)[statsigConfigID].(map[string]any)
	value := config["value"].(map[string]any)
	models := map[string]bool{}
	for _, raw := range value["available_models"].([]any) {
		models[raw.(string)] = true
	}
	for _, slug := range requiredPickerModelSlugs() {
		if !models[slug] {
			t.Fatalf("available_models missing %s: %+v", slug, value["available_models"])
		}
	}
	if pinnedInner["time"] != statsigFutureTimestampMillis || pinnedInner["company_lcut"] != statsigFutureTimestampMillis {
		t.Fatalf("inner evaluation timestamps were not pinned: %+v", pinnedInner)
	}

	var lastModified map[string]any
	if err := json.Unmarshal(rawLastModified[1:], &lastModified); err != nil {
		t.Fatal(err)
	}
	if lastModified["evaluations"] != statsigFutureTimestampMillis {
		t.Fatalf("last modified timestamp was not pinned: %+v", lastModified)
	}
}

func TestInspectStatsigSeparatesModelPresenceFromPinning(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "leveldb")
	db, err := leveldb.OpenFile(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := map[string]any{
		"dynamic_configs": map[string]any{
			statsigConfigID: map[string]any{
				"value": map[string]any{
					"available_models": stringSliceToAny(requiredPickerModelSlugs()),
				},
			},
		},
	}
	innerData, _ := json.Marshal(inner)
	outerData, _ := json.Marshal(map[string]any{"source": "NetworkNotModified", "receivedAt": float64(1), "data": string(innerData)})
	if err := db.Put([]byte("_app://-\x00\x01statsig.cached.evaluations.present-but-unpinned"), append([]byte{1}, outerData...), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	stats, changed, err := inspectOrPatchStatsigOnce(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected unpinned Statsig record to report a pending change")
	}
	if !stats.ModelsPresent {
		t.Fatalf("models should be present: %+v", stats)
	}
	if stats.FullyPinned || stats.AllPresent {
		t.Fatalf("unpinned cache should not be fully present: %+v", stats)
	}
	if stats.Incomplete != 0 {
		t.Fatalf("model presence should not be marked incomplete: %+v", stats)
	}
}

func TestPatchStatsigLastModifiedPreservesNonNumericFields(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"evaluations": float64(1),
		"schema":      "keep-me",
	})
	out, changed, err := patchStatsigLastModifiedBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected numeric last_modified timestamp to be pinned")
	}
	var patched map[string]any
	if err := json.Unmarshal(out, &patched); err != nil {
		t.Fatal(err)
	}
	if patched["evaluations"] != statsigFutureTimestampMillis {
		t.Fatalf("numeric timestamp was not pinned: %+v", patched)
	}
	if patched["schema"] != "keep-me" {
		t.Fatalf("non-numeric field was modified: %+v", patched)
	}
}

func stringSliceToAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func TestPrintableKeyEscapesControlBytes(t *testing.T) {
	got := printableKey([]byte("_app://-\x00\x01statsig.cached.evaluations.test"))
	want := `_app://-\x00\x01statsig.cached.evaluations.test`
	if got != want {
		t.Fatalf("printableKey = %q, want %q", got, want)
	}
}
