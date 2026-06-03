package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
)

const (
	sparkSlug = "gpt-5.3-codex-spark"
	flashSlug = "deepseek-v4-flash"
	proSlug   = "deepseek-v4-pro"
	humanSlug = "human-llm"

	flashModelName = "deepseek-v4-flash"
	proModelName   = "deepseek-v4-pro"
	humanModelName = "human-llm"

	statsigConfigID = "107580212"

	statsigPinnedSource          = "CachePinned"
	statsigFutureTimestampMillis = float64(4070908800000)
)

type options struct {
	command        string
	codexHome      string
	catalogPath    string
	statsigPath    string
	waitForAppExit bool
	skipStatsig    bool
	openAfter      bool
	timeout        time.Duration
}

type result struct {
	Name    string
	Status  string
	Details []string
}

type tokenfluxModel struct {
	Slug                   string
	ModelName              string
	DisplayName            string
	Description            string
	Priority               int
	DefaultReasoningLevel  string
	ReasoningLevels        []reasoningLevel
	SupportsReasoningNotes bool
}

type reasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type truncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int64  `json:"limit"`
}

type catalogModel struct {
	Slug                        string           `json:"slug"`
	DisplayName                 string           `json:"display_name"`
	Description                 string           `json:"description"`
	DefaultReasoningLevel       string           `json:"default_reasoning_level"`
	SupportedReasoningLevels    []reasoningLevel `json:"supported_reasoning_levels"`
	ShellType                   string           `json:"shell_type"`
	Visibility                  string           `json:"visibility"`
	SupportedInAPI              bool             `json:"supported_in_api"`
	Priority                    int              `json:"priority"`
	AdditionalSpeedTiers        []string         `json:"additional_speed_tiers"`
	AvailabilityNux             any              `json:"availability_nux"`
	Upgrade                     any              `json:"upgrade"`
	BaseInstructions            string           `json:"base_instructions"`
	SupportsReasoningSummaries  bool             `json:"supports_reasoning_summaries"`
	DefaultReasoningSummary     string           `json:"default_reasoning_summary"`
	SupportVerbosity            bool             `json:"support_verbosity"`
	DefaultVerbosity            *string          `json:"default_verbosity"`
	ApplyPatchToolType          string           `json:"apply_patch_tool_type"`
	WebSearchToolType           string           `json:"web_search_tool_type"`
	TruncationPolicy            truncationPolicy `json:"truncation_policy"`
	SupportsParallelToolCalls   bool             `json:"supports_parallel_tool_calls"`
	SupportsImageDetailOriginal bool             `json:"supports_image_detail_original"`
	ContextWindow               int              `json:"context_window"`
	MaxContextWindow            int              `json:"max_context_window"`
	AutoCompactTokenLimit       *int             `json:"auto_compact_token_limit,omitempty"`
	EffectiveContextWindowPct   int              `json:"effective_context_window_percent"`
	ExperimentalSupportedTools  []string         `json:"experimental_supported_tools"`
	InputModalities             []string         `json:"input_modalities"`
	SupportsSearchTool          bool             `json:"supports_search_tool"`
	ModelMessages               map[string]any   `json:"model_messages"`
}

var tokenfluxModels = []tokenfluxModel{
	{
		Slug:                   flashSlug,
		ModelName:              flashModelName,
		DisplayName:            "DeepSeek V4 Flash",
		Description:            "DeepSeek V4 Flash via tokenflux/CPA.",
		Priority:               12,
		DefaultReasoningLevel:  "high",
		ReasoningLevels:        deepseekReasoningLevels(),
		SupportsReasoningNotes: true,
	},
	{
		Slug:                   proSlug,
		ModelName:              proModelName,
		DisplayName:            "DeepSeek V4 Pro",
		Description:            "DeepSeek V4 Pro via tokenflux/CPA.",
		Priority:               13,
		DefaultReasoningLevel:  "high",
		ReasoningLevels:        deepseekReasoningLevels(),
		SupportsReasoningNotes: true,
	},
	{
		Slug:                   humanSlug,
		ModelName:              humanModelName,
		DisplayName:            "Human LLM",
		Description:            "Human-in-the-loop Responses model via tokenflux/CPA.",
		Priority:               14,
		DefaultReasoningLevel:  "medium",
		ReasoningLevels:        codexReasoningLevels(),
		SupportsReasoningNotes: false,
	},
}

var existingCatalogModelSlugs = []string{
	sparkSlug,
}

func requiredPickerModelSlugs() []string {
	slugs := make([]string, 0, len(existingCatalogModelSlugs)+len(tokenfluxModels))
	slugs = append(slugs, existingCatalogModelSlugs...)
	for _, spec := range tokenfluxModels {
		slugs = append(slugs, spec.Slug)
	}
	return slugs
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		exitErr(err)
	}

	results, err := run(opts)
	for _, r := range results {
		printResult(r)
	}
	if err != nil {
		exitErr(err)
	}
}

func parseArgs(args []string) (options, error) {
	if len(args) == 0 {
		args = []string{"install"}
	}
	cmd := args[0]
	if !validCommand(cmd) {
		return options{}, fmt.Errorf("unknown command %q; use install, plan, apply, verify, status, doctor, or repair", cmd)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return options{}, err
	}

	opts := options{
		command:     cmd,
		codexHome:   filepath.Join(home, ".codex"),
		catalogPath: filepath.Join(home, ".codex", "models_catalog.json"),
		statsigPath: defaultStatsigPath(home),
		timeout:     5 * time.Minute,
	}

	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.StringVar(&opts.codexHome, "codex-home", opts.codexHome, "Codex home directory")
	fs.StringVar(&opts.catalogPath, "catalog-path", opts.catalogPath, "models_catalog.json path")
	fs.StringVar(&opts.statsigPath, "statsig-path", opts.statsigPath, "Codex App Statsig LevelDB path")
	fs.BoolVar(&opts.waitForAppExit, "wait-for-app-exit", false, "wait until Codex App releases the Statsig LevelDB lock")
	fs.BoolVar(&opts.skipStatsig, "skip-statsig", false, "skip Codex App Statsig available_models patch")
	fs.BoolVar(&opts.openAfter, "open-after", false, "open Codex App after apply/repair/install")
	fs.DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum wait time for Codex App to quit during install/repair")
	if err := fs.Parse(args[1:]); err != nil {
		return options{}, err
	}
	opts.configPathNormalize()
	return opts, nil
}

func defaultStatsigPath(home string) string {
	defaultPath := filepath.Join(home, "Library", "Application Support", "Codex", "Default", "Local Storage", "leveldb")
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return filepath.Join(home, "Library", "Application Support", "Codex", "Local Storage", "leveldb")
}

func validCommand(command string) bool {
	switch command {
	case "install", "plan", "apply", "verify", "status", "doctor", "repair":
		return true
	default:
		return false
	}
}

func readOnlyCommand(command string) bool {
	switch command {
	case "plan", "verify", "status", "doctor":
		return true
	default:
		return false
	}
}

func patchCommand(command string) bool {
	return command == "apply" || command == "repair"
}

func statusCommand(command string) bool {
	return command == "status" || command == "doctor"
}

func (o *options) configPathNormalize() {
	if !filepath.IsAbs(o.codexHome) {
		if abs, err := filepath.Abs(o.codexHome); err == nil {
			o.codexHome = abs
		}
	}
	if !filepath.IsAbs(o.catalogPath) {
		if abs, err := filepath.Abs(o.catalogPath); err == nil {
			o.catalogPath = abs
		}
	}
	if !filepath.IsAbs(o.statsigPath) {
		if abs, err := filepath.Abs(o.statsigPath); err == nil {
			o.statsigPath = abs
		}
	}
}

func run(opts options) ([]result, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("this repair tool is macOS-only; verify the real Codex app cache path and schema before adding support for another OS")
	}

	if opts.command == "install" {
		return runInstall(opts)
	}

	configPath := filepath.Join(opts.codexHome, "config.toml")
	var results []result

	configReport, configPatched, err := handleConfig(configPath, opts.catalogPath, opts.command)
	results = append(results, configReport)
	if err != nil {
		return results, err
	}
	needsRepair := configPatched

	catalogReport, catalogPatched, err := handleCatalog(opts.catalogPath, opts.command)
	results = append(results, catalogReport)
	if err != nil {
		return results, err
	}
	needsRepair = needsRepair || catalogPatched

	if opts.skipStatsig {
		results = append(results, result{
			Name:   "Statsig",
			Status: "SKIPPED",
			Details: []string{
				"--skip-statsig was set; Codex App picker may not show all required models until available_models is patched.",
			},
		})
		if opts.command == "doctor" && needsRepair {
			return results, errors.New("doctor found required model visibility is missing or incomplete; run repair")
		}
		return results, nil
	}

	statsigReport, err := handleStatsig(opts.statsigPath, opts.command, opts.waitForAppExit, opts.timeout)
	results = append(results, statsigReport)
	if err != nil {
		return results, err
	}
	needsRepair = needsRepair || statsigReport.Status == "NEEDS_REPAIR" || statsigReport.Status == "WOULD_CHANGE"

	if patchCommand(opts.command) && opts.openAfter {
		openReport, err := openCodexAfterVerify(opts.statsigPath)
		results = append(results, openReport)
		if err != nil {
			return results, err
		}
	}

	if opts.command == "doctor" && needsRepair {
		return results, errors.New("doctor found required model visibility is missing or incomplete; run repair")
	}
	return results, nil
}

func runInstall(opts options) ([]result, error) {
	configPath := filepath.Join(opts.codexHome, "config.toml")
	var results []result

	results = append(results, result{
		Name:    "Install",
		Status:  "START",
		Details: installStartDetails(opts),
	})

	configReport, _, err := handleConfig(configPath, opts.catalogPath, "apply")
	results = append(results, configReport)
	if err != nil {
		return results, err
	}

	catalogReport, _, err := handleCatalog(opts.catalogPath, "apply")
	results = append(results, catalogReport)
	if err != nil {
		return results, err
	}

	if !opts.skipStatsig {
		results = append(results, result{
			Name:   "Codex App",
			Status: "WAIT",
			Details: []string{
				"If Codex App is open, quit it with Cmd+Q now. Closing the window is not enough.",
				fmt.Sprintf("Waiting up to %s for LevelDB lock release.", opts.timeout),
			},
		})
		if err := waitForLevelDB(opts.statsigPath, opts.timeout); err != nil {
			results = append(results, result{Name: "Statsig", Status: "FAIL", Details: []string{err.Error()}})
			return results, err
		}
		statsigReport, err := handleStatsig(opts.statsigPath, "apply", false, opts.timeout)
		results = append(results, statsigReport)
		if err != nil {
			return results, err
		}
	} else {
		results = append(results, result{
			Name:   "Statsig",
			Status: "SKIPPED",
			Details: []string{
				"--skip-statsig was set; Codex App picker may not show all required models until available_models is patched.",
			},
		})
	}

	verifyReport, err := runVerifyAfterInstall(opts)
	results = append(results, verifyReport...)
	if err != nil {
		return results, err
	}

	if opts.openAfter && !opts.skipStatsig {
		openReport, err := openCodexAfterVerify(opts.statsigPath)
		results = append(results, openReport)
		if err != nil {
			return results, err
		}
	}

	results = append(results, result{
		Name:    "Install",
		Status:  "DONE",
		Details: installDoneDetails(opts),
	})
	return results, nil
}

func openCodexAfterVerify(statsigPath string) (result, error) {
	verifyReport, err := handleStatsig(statsigPath, "verify", false, 0)
	if err != nil {
		details := []string{"Refusing to open Codex App because the current picker cache did not verify."}
		details = append(details, verifyReport.Details...)
		return result{Name: "Open Codex", Status: "FAIL", Details: details}, err
	}
	if err := exec.Command("open", "-a", "Codex").Run(); err != nil {
		return result{Name: "Open Codex", Status: "WARN", Details: []string{err.Error()}}, nil
	}
	return result{Name: "Open Codex", Status: "OK", Details: []string{"Codex App open command sent after verifying all required models are present in Statsig."}}, nil
}

func installStartDetails(opts options) []string {
	details := []string{"Installing TokenFlux/CPA model visibility for existing tokenflux Codex config."}
	if opts.skipStatsig {
		return append(details, "Config/catalog will be patched; Statsig/App picker patch is skipped.")
	}
	return append(details, "Config/catalog will be patched first. Then Statsig/App picker gets a one-time local injection after Codex App fully quits.")
}

func installDoneDetails(opts options) []string {
	if opts.skipStatsig {
		return []string{"Config/catalog are configured. Statsig/App picker patch was skipped."}
	}
	return []string{"TokenFlux/CPA model catalog and Codex App picker allowlist are configured. If Codex App later refreshes Statsig from network and a required model disappears, run repair."}
}

func runVerifyAfterInstall(opts options) ([]result, error) {
	verifyOpts := opts
	verifyOpts.command = "verify"
	return run(verifyOpts)
}

func handleConfig(configPath, catalogPath, command string) (result, bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return result{Name: "Config", Status: "FAIL", Details: []string{fmt.Sprintf("config.toml is required: %s", configPath)}}, false, err
	}

	provider, ok := topLevelValue(string(data), "model_provider")
	if !ok {
		return result{Name: "Config", Status: "FAIL", Details: []string{"top-level model_provider is missing; this installer only supports existing tokenflux configs."}}, false, errors.New("missing top-level model_provider")
	}
	if provider != "tokenflux" {
		return result{Name: "Config", Status: "FAIL", Details: []string{fmt.Sprintf("model_provider is %q, expected %q.", provider, "tokenflux")}}, false, fmt.Errorf("unsupported model_provider %q", provider)
	}

	updated, changed, err := patchModelCatalogJSONLine(string(data), catalogPath)
	if err != nil {
		return result{Name: "Config", Status: "FAIL", Details: []string{err.Error()}}, false, err
	}
	if command == "verify" && changed {
		return result{Name: "Config", Status: "FAIL", Details: []string{fmt.Sprintf("model_catalog_json is not set to %s", catalogPath)}}, false, errors.New("config verification failed")
	}
	if statusCommand(command) {
		status := "OK"
		details := []string{"model_provider is tokenflux."}
		if changed {
			status = "NEEDS_REPAIR"
			details = append(details, fmt.Sprintf("model_catalog_json is not set to %s; run repair.", catalogPath))
		} else {
			details = append(details, fmt.Sprintf("model_catalog_json points to %s.", catalogPath))
		}
		return result{Name: "Config", Status: status, Details: details}, changed, nil
	}
	if command == "plan" {
		status := "OK"
		details := []string{"model_provider is tokenflux."}
		if changed {
			status = "WOULD_CHANGE"
			details = append(details, fmt.Sprintf("Would set model_catalog_json to %s.", catalogPath))
		} else {
			details = append(details, fmt.Sprintf("model_catalog_json already points to %s.", catalogPath))
		}
		return result{Name: "Config", Status: status, Details: details}, changed, nil
	}
	if patchCommand(command) && changed {
		if err := backupFile(configPath); err != nil {
			return result{Name: "Config", Status: "FAIL", Details: []string{fmt.Sprintf("backup failed: %v", err)}}, false, err
		}
		if err := os.WriteFile(configPath, []byte(updated), 0600); err != nil {
			return result{Name: "Config", Status: "FAIL", Details: []string{err.Error()}}, false, err
		}
		return result{Name: "Config", Status: "PATCHED", Details: []string{fmt.Sprintf("model_catalog_json set to %s.", catalogPath)}}, true, nil
	}
	return result{Name: "Config", Status: "OK", Details: []string{"model_provider is tokenflux.", fmt.Sprintf("model_catalog_json points to %s.", catalogPath)}}, false, nil
}

func topLevelValue(tomlText, key string) (string, bool) {
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=\s*"([^"]*)"\s*(?:#.*)?$`)
	for _, line := range strings.Split(tomlText, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			return "", false
		}
		if m := re.FindStringSubmatch(line); len(m) == 2 {
			return m[1], true
		}
	}
	return "", false
}

func patchModelCatalogJSONLine(tomlText, catalogPath string) (string, bool, error) {
	lines := strings.SplitAfter(tomlText, "\n")
	re := regexp.MustCompile(`^(\s*)model_catalog_json\s*=.*$`)
	sectionStart := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			sectionStart = i
			break
		}
	}
	newLine := fmt.Sprintf("model_catalog_json = %q\n", catalogPath)
	for i := 0; i < sectionStart; i++ {
		raw := strings.TrimRight(lines[i], "\r\n")
		if !re.MatchString(raw) {
			continue
		}
		if raw == strings.TrimRight(newLine, "\n") {
			return tomlText, false, nil
		}
		lines[i] = newLine
		return strings.Join(lines, ""), true, nil
	}

	insertAt := 0
	for i := 0; i < sectionStart; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "model_provider") {
			insertAt = i + 1
			break
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, newLine)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, ""), true, nil
}

func handleCatalog(catalogPath, command string) (result, bool, error) {
	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return result{Name: "Catalog", Status: "FAIL", Details: []string{fmt.Sprintf("models_catalog.json is required: %s", catalogPath)}}, false, err
	}

	updated, changed, report, err := patchCatalog(data)
	if err != nil {
		return result{Name: "Catalog", Status: "FAIL", Details: append([]string{err.Error()}, report...)}, false, err
	}
	if command == "verify" && changed {
		return result{Name: "Catalog", Status: "FAIL", Details: append([]string{"TokenFlux/CPA model objects are missing or incomplete."}, report...)}, false, errors.New("catalog verification failed")
	}
	if statusCommand(command) {
		status := "OK"
		if changed {
			status = "NEEDS_REPAIR"
			report = append(report, "TokenFlux/CPA model objects are missing or incomplete; run repair.")
		}
		return result{Name: "Catalog", Status: status, Details: report}, changed, nil
	}
	if command == "plan" {
		status := "OK"
		if changed {
			status = "WOULD_CHANGE"
		}
		return result{Name: "Catalog", Status: status, Details: report}, changed, nil
	}
	if patchCommand(command) && changed {
		if err := backupFile(catalogPath); err != nil {
			return result{Name: "Catalog", Status: "FAIL", Details: []string{fmt.Sprintf("backup failed: %v", err)}}, false, err
		}
		if err := os.WriteFile(catalogPath, updated, 0600); err != nil {
			return result{Name: "Catalog", Status: "FAIL", Details: []string{err.Error()}}, false, err
		}
		return result{Name: "Catalog", Status: "PATCHED", Details: report}, true, nil
	}
	return result{Name: "Catalog", Status: "OK", Details: report}, false, nil
}

func patchCatalog(data []byte) ([]byte, bool, []string, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false, nil, fmt.Errorf("catalog JSON parse failed: %w", err)
	}
	rawModels, ok := root["models"].([]any)
	if !ok || len(rawModels) == 0 {
		return nil, false, nil, errors.New("catalog must contain a non-empty models array")
	}

	models := make([]map[string]any, 0, len(rawModels))
	for i, raw := range rawModels {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, false, nil, fmt.Errorf("models[%d] is not an object", i)
		}
		models = append(models, m)
	}

	template, err := selectTemplateModel(models)
	if err != nil {
		return nil, false, nil, err
	}

	changed := false
	report := []string{fmt.Sprintf("Using %q as Codex prompt/tool template.", stringField(template, "slug"))}
	for _, spec := range tokenfluxModels {
		next := buildTokenfluxModel(template, spec)
		idx := indexModel(models, spec.Slug)
		if idx < 0 {
			models = append(models, next)
			changed = true
			report = append(report, fmt.Sprintf("Will add %s.", spec.Slug))
			continue
		}
		if !modelObjectComplete(models[idx], spec.Slug) {
			models[idx] = next
			changed = true
			report = append(report, fmt.Sprintf("Will replace incomplete %s.", spec.Slug))
		} else if !reflect.DeepEqual(models[idx], next) {
			models[idx] = next
			changed = true
			report = append(report, fmt.Sprintf("Will refresh %s from latest Codex prompt/tool template.", spec.Slug))
		} else {
			report = append(report, fmt.Sprintf("%s already complete.", spec.Slug))
		}
	}
	for _, slug := range existingCatalogModelSlugs {
		idx := indexModel(models, slug)
		if idx < 0 {
			report = append(report, fmt.Sprintf("%s is missing from the existing Codex catalog.", slug))
			return nil, false, report, fmt.Errorf("catalog missing required existing model %s", slug)
		}
		if !modelObjectComplete(models[idx], slug) {
			report = append(report, fmt.Sprintf("%s exists but is incomplete in the existing Codex catalog.", slug))
			return nil, false, report, fmt.Errorf("catalog has incomplete required existing model %s", slug)
		}
		report = append(report, fmt.Sprintf("%s already present.", slug))
	}

	outModels := make([]any, len(models))
	for i, m := range models {
		outModels[i] = m
	}
	root["models"] = outModels
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, nil, err
	}
	out = append(out, '\n')
	return out, changed, report, nil
}

func selectTemplateModel(models []map[string]any) (map[string]any, error) {
	preferred := []string{"gpt-5.5", "gpt-5.4", "gpt-5.3-codex", "gpt-5.2"}
	for _, slug := range preferred {
		if idx := indexModel(models, slug); idx >= 0 && modelHasPromptFields(models[idx]) {
			return models[idx], nil
		}
	}
	for _, m := range models {
		if modelHasPromptFields(m) {
			return m, nil
		}
	}
	return nil, errors.New("no existing model with base_instructions and model_messages.instructions_template found; refusing to inject bare TokenFlux/CPA model catalog objects")
}

func modelHasPromptFields(m map[string]any) bool {
	if strings.TrimSpace(stringField(m, "base_instructions")) == "" {
		return false
	}
	mm, ok := m["model_messages"].(map[string]any)
	if !ok {
		return false
	}
	return strings.TrimSpace(stringField(mm, "instructions_template")) != ""
}

func buildTokenfluxModel(template map[string]any, spec tokenfluxModel) map[string]any {
	next := deepCopyMap(template)
	transformPromptValues(next, spec.ModelName)

	model := newTokenfluxCatalogModel(template, spec)
	data, _ := json.Marshal(model)
	var canonical map[string]any
	_ = json.Unmarshal(data, &canonical)
	for key, value := range canonical {
		next[key] = value
	}
	return next
}

func newTokenfluxCatalogModel(template map[string]any, spec tokenfluxModel) catalogModel {
	modelMessages := map[string]any{}
	if raw, ok := template["model_messages"].(map[string]any); ok {
		modelMessages = deepCopyMap(raw)
	}
	transformPromptValues(modelMessages, spec.ModelName)

	return catalogModel{
		Slug:                       spec.Slug,
		DisplayName:                spec.DisplayName,
		Description:                spec.Description,
		DefaultReasoningLevel:      spec.DefaultReasoningLevel,
		SupportedReasoningLevels:   spec.ReasoningLevels,
		ShellType:                  "unified_exec",
		Visibility:                 "list",
		SupportedInAPI:             true,
		Priority:                   spec.Priority,
		AdditionalSpeedTiers:       []string{},
		AvailabilityNux:            nil,
		Upgrade:                    nil,
		BaseInstructions:           transformPrompt(stringField(template, "base_instructions"), spec.ModelName),
		SupportsReasoningSummaries: spec.SupportsReasoningNotes,
		DefaultReasoningSummary:    "auto",
		SupportVerbosity:           false,
		DefaultVerbosity:           nil,
		ApplyPatchToolType:         "freeform",
		WebSearchToolType:          "text",
		TruncationPolicy: truncationPolicy{
			Mode:  "tokens",
			Limit: 10000,
		},
		SupportsParallelToolCalls:   true,
		SupportsImageDetailOriginal: false,
		ContextWindow:               1000000,
		MaxContextWindow:            1000000,
		EffectiveContextWindowPct:   95,
		ExperimentalSupportedTools:  []string{},
		InputModalities:             []string{"text"},
		SupportsSearchTool:          false,
		ModelMessages:               modelMessages,
	}
}

func deepseekReasoningLevels() []reasoningLevel {
	return []reasoningLevel{
		{Effort: "high", Description: "High reasoning effort"},
		{Effort: "xhigh", Description: "Extra high reasoning effort (maps to DeepSeek max)"},
	}
}

func codexReasoningLevels() []reasoningLevel {
	return []reasoningLevel{
		{Effort: "low", Description: "Fast responses with lighter reasoning"},
		{Effort: "medium", Description: "Balances speed and reasoning depth for everyday tasks"},
		{Effort: "high", Description: "Greater reasoning depth for complex problems"},
		{Effort: "xhigh", Description: "Extra high reasoning depth for complex problems"},
	}
}

func transformPromptValues(v any, modelName string) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if s, ok := child.(string); ok {
				x[k] = transformPrompt(s, modelName)
			} else {
				transformPromptValues(child, modelName)
			}
		}
	case []any:
		for _, child := range x {
			transformPromptValues(child, modelName)
		}
	}
}

func transformPrompt(s, modelName string) string {
	if s == "" {
		return s
	}
	re := regexp.MustCompile(`(?s)^You are Codex, a coding agent based on GPT-5\.[^\n]*(?:\n\n|$)`)
	replacement := fmt.Sprintf("You are %s, a coding agent. You and the user share one workspace, and your job is to collaborate with them until their goal is genuinely handled.\n\n", modelName)
	s = re.ReplaceAllString(s, replacement)
	re = regexp.MustCompile(`(?s)^You are [^,\n]+, a coding agent\.[^\n]*(?:\n\n|$)`)
	s = re.ReplaceAllString(s, replacement)
	s = strings.ReplaceAll(s, "You have a vivid inner life as Codex:", "You have a vivid inner life as a coding agent:")
	s = strings.ReplaceAll(s, "You have a vivid inner life as Codex", "You have a vivid inner life as a coding agent")
	return s
}

func modelObjectComplete(m map[string]any, slug string) bool {
	return stringField(m, "slug") == slug &&
		stringField(m, "visibility") == "list" &&
		boolField(m, "supported_in_api") &&
		strings.TrimSpace(stringField(m, "base_instructions")) != "" &&
		modelHasPromptFields(m)
}

func handleStatsig(dbPath, command string, wait bool, timeout time.Duration) (result, error) {
	if runtime.GOOS != "darwin" {
		return result{Name: "Statsig", Status: "FAIL", Details: []string{"Statsig patch is only supported on macOS in this version."}}, errors.New("unsupported platform")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return result{Name: "Statsig", Status: "FAIL", Details: []string{fmt.Sprintf("Codex App Local Storage LevelDB not found: %s", dbPath), "Open Codex App once, then quit it with Cmd+Q and retry."}}, err
	}

	if wait {
		if err := waitForLevelDB(dbPath, timeout); err != nil {
			return result{Name: "Statsig", Status: "FAIL", Details: []string{err.Error()}}, err
		}
	}

	locked := false
	if err := assertLevelDBUnlocked(dbPath); err != nil {
		if readOnlyCommand(command) {
			locked = true
		} else {
			details := []string{
				err.Error(),
				"Quit Codex App with Cmd+Q. Closing the window is not enough.",
			}
			if owners := lockOwners(dbPath); len(owners) > 0 {
				details = append(details, "Lock/process hints:")
				details = append(details, owners...)
			}
			return result{Name: "Statsig", Status: "FAIL", Details: details}, err
		}
	}

	statsigPath := dbPath
	var snapshotDir string
	if locked {
		var err error
		snapshotDir, err = copyStatsigSnapshot(dbPath)
		if err != nil {
			details := []string{
				fmt.Sprintf("LevelDB is locked and snapshot inspection failed: %v", err),
				"Quit Codex App with Cmd+Q before repair/apply.",
			}
			if owners := lockOwners(dbPath); len(owners) > 0 {
				details = append(details, "Lock/process hints:")
				details = append(details, owners...)
			}
			return result{Name: "Statsig", Status: "FAIL", Details: details}, err
		}
		defer os.RemoveAll(snapshotDir)
		statsigPath = snapshotDir
	}

	stats, changed, err := inspectOrPatchStatsig(statsigPath, patchCommand(command))
	if locked {
		lockedDetails := []string{
			"LevelDB is locked by Codex App; inspected a temporary read-only snapshot.",
			"Quit Codex App with Cmd+Q before running repair/apply.",
		}
		if err != nil {
			if owners := lockOwners(dbPath); len(owners) > 0 {
				lockedDetails = append(lockedDetails, "Lock/process hints:")
				lockedDetails = append(lockedDetails, owners...)
			}
			return result{Name: "Statsig", Status: "FAIL", Details: append(lockedDetails, err.Error())}, err
		}
		statsDetails := stats.Details()
		statsDetails = append(lockedDetails, statsDetails...)
		return statsigResult(command, stats, changed, statsDetails)
	}
	if err != nil {
		return result{Name: "Statsig", Status: "FAIL", Details: []string{err.Error()}}, err
	}
	return statsigResult(command, stats, changed, stats.Details())
}

func statsigResult(command string, stats statsigStats, changed bool, details []string) (result, error) {
	if stats.SeenConfig == 0 {
		return result{Name: "Statsig", Status: "FAIL", Details: []string{"No Statsig dynamic config 107580212 found; refusing to fabricate unknown schema."}}, errors.New("statsig config not found")
	}
	if command == "verify" && !stats.AllPresent {
		return result{Name: "Statsig", Status: "FAIL", Details: details}, errors.New("statsig verification failed")
	}
	if statusCommand(command) {
		if !stats.ModelsPresent {
			return result{Name: "Statsig", Status: "NEEDS_REPAIR", Details: append(details, "Required models are missing from Statsig available_models; run repair after quitting Codex App.")}, nil
		}
		if !stats.FullyPinned {
			return result{Name: "Statsig", Status: "NEEDS_REPAIR", Details: append(details, "Required models are present, but the Statsig cache is not fully pinned; run repair after quitting Codex App.")}, nil
		}
		return result{Name: "Statsig", Status: "OK", Details: details}, nil
	}
	if command == "plan" {
		status := "OK"
		if !stats.AllPresent {
			status = "WOULD_CHANGE"
		}
		return result{Name: "Statsig", Status: status, Details: details}, nil
	}
	if patchCommand(command) {
		if changed {
			return result{Name: "Statsig", Status: "PATCHED", Details: details}, nil
		}
		return result{Name: "Statsig", Status: "OK", Details: details}, nil
	}
	return result{Name: "Statsig", Status: "OK", Details: details}, nil
}

func copyStatsigSnapshot(dbPath string) (string, error) {
	tmp, err := os.MkdirTemp("", "codex-tokenflux-statsig-*")
	if err != nil {
		return "", err
	}
	if err := copyDir(dbPath, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func waitForLevelDB(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := assertLevelDBUnlocked(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Codex App to release LevelDB lock: %s", path)
		}
		time.Sleep(1 * time.Second)
	}
}

func assertLevelDBUnlocked(path string) error {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return fmt.Errorf("LevelDB is locked or unreadable: %w", err)
	}
	return db.Close()
}

type statsigStats struct {
	SeenConfig         int
	Patched            int
	Already            int
	Incomplete         int
	SeenLastModified   int
	PinnedLastModified int
	ModelsPresent      bool
	FullyPinned        bool
	AllPresent         bool
	Keys               []string
	LastModifiedKeys   []string
}

func (s statsigStats) Details() []string {
	d := []string{
		fmt.Sprintf("seenConfig=%d changeCount=%d alreadyComplete=%d incomplete=%d seenLastModified=%d pinnedLastModified=%d modelsPresent=%v fullyPinned=%v allPresent=%v", s.SeenConfig, s.Patched, s.Already, s.Incomplete, s.SeenLastModified, s.PinnedLastModified, s.ModelsPresent, s.FullyPinned, s.AllPresent),
	}
	if len(s.Keys) > 0 {
		sort.Strings(s.Keys)
		d = append(d, "matched keys:")
		d = append(d, s.Keys...)
	}
	if len(s.LastModifiedKeys) > 0 {
		sort.Strings(s.LastModifiedKeys)
		d = append(d, "last-modified keys:")
		d = append(d, s.LastModifiedKeys...)
	}
	return d
}

func inspectOrPatchStatsig(path string, write bool) (statsigStats, bool, error) {
	if write {
		if err := backupDir(path); err != nil {
			return statsigStats{}, false, fmt.Errorf("statsig backup failed: %w", err)
		}
	}
	stats, changed, err := inspectOrPatchStatsigOnce(path, write)
	if err != nil {
		return statsigStats{}, false, err
	}
	if write && changed {
		verified, _, err := inspectOrPatchStatsigOnce(path, false)
		if err != nil {
			return stats, true, err
		}
		stats.Already = verified.Already
		stats.Incomplete = verified.Incomplete
		stats.ModelsPresent = verified.ModelsPresent
		stats.FullyPinned = verified.FullyPinned
		stats.AllPresent = verified.AllPresent
	}
	return stats, changed, nil
}

func inspectOrPatchStatsigOnce(path string, write bool) (statsigStats, bool, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return statsigStats{}, false, err
	}
	defer db.Close()

	var stats statsigStats
	iter := db.NewIterator(nil, nil)
	defer iter.Release()
	for iter.Next() {
		key := append([]byte(nil), iter.Key()...)
		value := append([]byte(nil), iter.Value()...)
		if bytes.Contains(key, []byte("statsig.last_modified_time.evaluations")) {
			body, prefix := splitStoredValue(value)
			patchedBody, changed, err := patchStatsigLastModifiedBody(body)
			if err != nil {
				continue
			}
			stats.SeenLastModified++
			stats.LastModifiedKeys = append(stats.LastModifiedKeys, printableKey(key))
			if changed {
				if write {
					if err := db.Put(key, append([]byte(prefix), patchedBody...), nil); err != nil {
						return stats, false, err
					}
				}
				stats.Patched++
				stats.PinnedLastModified++
			}
			continue
		}
		if !bytes.Contains(key, []byte("statsig.cached.evaluations")) {
			continue
		}
		body, prefix := splitStoredValue(value)
		patchedBody, seen, changed, modelsComplete, fullyPinned, err := patchStatsigBody(body)
		if err != nil {
			continue
		}
		if !seen {
			continue
		}
		stats.SeenConfig++
		stats.Keys = append(stats.Keys, printableKey(key))
		if modelsComplete && fullyPinned && !changed {
			stats.Already++
		}
		if !modelsComplete {
			stats.Incomplete++
		}
		if changed {
			if write {
				if err := db.Put(key, append([]byte(prefix), patchedBody...), nil); err != nil {
					return stats, false, err
				}
			}
			stats.Patched++
		}
	}
	if err := iter.Error(); err != nil {
		return stats, false, err
	}
	stats.ModelsPresent = stats.SeenConfig > 0 && stats.Incomplete == 0
	stats.FullyPinned = stats.ModelsPresent && stats.Patched == 0
	stats.AllPresent = stats.ModelsPresent && stats.FullyPinned
	return stats, stats.Patched > 0, nil
}

func printableKey(key []byte) string {
	var b strings.Builder
	for _, c := range key {
		if c >= 0x20 && c <= 0x7e {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "\\x%02x", c)
	}
	return b.String()
}

func splitStoredValue(value []byte) ([]byte, string) {
	if len(value) > 0 && value[0] <= 0x1f {
		return value[1:], string(value[:1])
	}
	return value, ""
}

func patchStatsigBody(body []byte) ([]byte, bool, bool, bool, bool, error) {
	var outer map[string]any
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, false, false, false, false, err
	}
	dataText, ok := outer["data"].(string)
	if !ok {
		return nil, false, false, false, false, nil
	}
	var inner map[string]any
	if err := json.Unmarshal([]byte(dataText), &inner); err != nil {
		return nil, false, false, false, false, err
	}
	dyn, ok := inner["dynamic_configs"].(map[string]any)
	if !ok {
		return nil, false, false, false, false, nil
	}
	cfg, ok := dyn[statsigConfigID].(map[string]any)
	if !ok {
		return nil, false, false, false, false, nil
	}
	value, ok := cfg["value"].(map[string]any)
	if !ok {
		return nil, true, false, false, false, nil
	}
	rawModels, ok := value["available_models"].([]any)
	if !ok {
		return nil, true, false, false, false, nil
	}
	existing := map[string]bool{}
	for _, raw := range rawModels {
		if s, ok := raw.(string); ok {
			existing[s] = true
		}
	}
	changed := false
	modelsComplete := true
	for _, slug := range requiredPickerModelSlugs() {
		if !existing[slug] {
			modelsComplete = false
			rawModels = append(rawModels, slug)
			changed = true
		}
	}
	fullyPinned := outer["source"] == statsigPinnedSource &&
		numberPinned(outer, "receivedAt") &&
		numberPinned(outer, "time") &&
		numberPinned(inner, "time") &&
		numberPinned(inner, "company_lcut")
	if outer["source"] != statsigPinnedSource {
		outer["source"] = statsigPinnedSource
		changed = true
	}
	if pinNumber(outer, "receivedAt") {
		changed = true
	}
	if pinNumber(outer, "time") {
		changed = true
	}
	if pinNumber(inner, "time") {
		changed = true
	}
	if pinNumber(inner, "company_lcut") {
		changed = true
	}
	if !changed {
		return body, true, false, modelsComplete, fullyPinned, nil
	}
	value["available_models"] = rawModels
	innerData, err := json.Marshal(inner)
	if err != nil {
		return nil, true, false, false, false, err
	}
	outer["data"] = string(innerData)
	out, err := json.Marshal(outer)
	if err != nil {
		return nil, true, false, false, false, err
	}
	return out, true, true, modelsComplete, false, nil
}

func patchStatsigLastModifiedBody(body []byte) ([]byte, bool, error) {
	var outer map[string]any
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, false, err
	}
	changed := false
	for key := range outer {
		if pinExistingNumber(outer, key) {
			changed = true
		}
	}
	if !changed {
		return body, false, nil
	}
	out, err := json.Marshal(outer)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func pinNumber(object map[string]any, key string) bool {
	current, ok := numericValue(object[key])
	if !ok || current < statsigFutureTimestampMillis {
		object[key] = statsigFutureTimestampMillis
		return true
	}
	return false
}

func pinExistingNumber(object map[string]any, key string) bool {
	current, ok := numericValue(object[key])
	if ok && current < statsigFutureTimestampMillis {
		object[key] = statsigFutureTimestampMillis
		return true
	}
	return false
}

func numberPinned(object map[string]any, key string) bool {
	current, ok := numericValue(object[key])
	return ok && current >= statsigFutureTimestampMillis
}

func numericValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func lockOwners(dbPath string) []string {
	lockPath := filepath.Join(dbPath, "LOCK")
	var out []string
	if b, err := exec.Command("lsof", lockPath).CombinedOutput(); err == nil && len(bytes.TrimSpace(b)) > 0 {
		out = append(out, string(bytes.TrimSpace(b)))
	}
	if b, err := exec.Command("pgrep", "-afil", "Codex.app|Code[x] Helper|codex app-server").CombinedOutput(); err == nil && len(bytes.TrimSpace(b)) > 0 {
		out = append(out, string(bytes.TrimSpace(b)))
	}
	return out
}

func indexModel(models []map[string]any, slug string) int {
	for i, m := range models {
		if stringField(m, "slug") == slug {
			return i
		}
	}
	return -1
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func boolField(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func deepCopyMap(in map[string]any) map[string]any {
	data, _ := json.Marshal(in)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func backupFile(path string) error {
	backupRoot, err := backupRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(backupRoot, 0700); err != nil {
		return err
	}
	dst := filepath.Join(backupRoot, filepath.Base(path))
	return copyFile(path, dst)
}

func backupDir(path string) error {
	backupRoot, err := backupRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(backupRoot, 0700); err != nil {
		return err
	}
	dst := filepath.Join(backupRoot, filepath.Base(path))
	return copyDir(path, dst)
}

func backupRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "tokenflux-model-installer-backups", time.Now().Format("20060102-150405")), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		return copyFile(path, target)
	})
}

func printResult(r result) {
	fmt.Printf("[%s] %s\n", r.Status, r.Name)
	for _, d := range r.Details {
		for _, line := range strings.Split(d, "\n") {
			fmt.Printf("  - %s\n", line)
		}
	}
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
