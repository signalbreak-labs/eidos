package generator

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// renderProvider renders internal/provider/provider.go for the supplied IR.
func renderProvider(t *testing.T, pir ir.ProviderIR) string {
	t.Helper()
	file, err := ProviderFileWithClient(pir, testClientImport)
	if err != nil {
		t.Fatalf("ProviderFileWithClient() error = %v", err)
	}
	var buf bytes.Buffer
	if err := file.Render(&buf); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return buf.String()
}

// TestProviderLoggingAttributes_Render asserts the provider schema exposes the
// generator-owned log_* trace-logging attributes (all Optional) and the config
// model struct carries the matching fields.
func TestProviderLoggingAttributes_Render(t *testing.T) {
	got := renderProvider(t, sampleProviderWithResourceIR())

	for _, want := range []string{
		`"log_file": schema.StringAttribute{`,
		`"log_capture_request_headers": schema.BoolAttribute{`,
		`"log_capture_request_body": schema.BoolAttribute{`,
		`"log_capture_response_headers": schema.BoolAttribute{`,
		`"log_capture_response_body": schema.BoolAttribute{`,
		`"log_max_body_bytes": schema.Int64Attribute{`,
		// Model struct fields Configure reads (gofmt aligns both columns, so
		// match the tfsdk tags, which appear only on model struct fields).
		"`tfsdk:\"log_file\"`",
		"`tfsdk:\"log_capture_request_headers\"`",
		"`tfsdk:\"log_capture_request_body\"`",
		"`tfsdk:\"log_capture_response_headers\"`",
		"`tfsdk:\"log_capture_response_body\"`",
		"`tfsdk:\"log_max_body_bytes\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("provider schema missing %q\n--- file ---\n%s", want, got)
		}
	}
	// Every log_* attribute must be Optional so existing practitioner configs
	// are unaffected.
	if strings.Contains(got, "log_file") && !strings.Contains(got, `Optional: true`) {
		t.Errorf("log_* attributes must be Optional\n--- file ---\n%s", got)
	}
}

// TestProviderLoggingAttributes_NoDuplicateOnCollision asserts a declared
// config attribute whose name collides with a log_* attribute wins: the
// logging attribute is not appended a second time (which would emit a
// duplicate schema map key and model field).
func TestProviderLoggingAttributes_NoDuplicateOnCollision(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	pir.ConfigSchema.Attributes = append(pir.ConfigSchema.Attributes, ir.AttributeIR{
		Name:     "log_file",
		Optional: true,
		Schema:   ir.SchemaIR{Type: ir.TypeString},
	})
	got := renderProvider(t, pir)

	if n := strings.Count(got, `"log_file": schema.StringAttribute{`); n != 1 {
		t.Errorf(`"log_file" attribute count = %d, want 1\n--- file ---\n%s`, n, got)
	}
	if strings.Contains(got, "client.WithLogging") {
		t.Errorf("colliding log_file must disable the WithLogging wiring\n--- file ---\n%s", got)
	}
}

// TestConfigure_WithLoggingCall_Render asserts the wired-provider Configure
// builds a client.LoggingConfig from the log_* model fields and appends
// client.WithLogging guarded on a log file being configured.
func TestConfigure_WithLoggingCall_Render(t *testing.T) {
	got := renderProvider(t, sampleProviderWithResourceIR())

	for _, want := range []string{
		`loggingConfig := client.LoggingConfig{`,
		`loggingConfig.LogFile = config.LogFile.ValueString()`,
		`loggingConfig.CaptureRequestHeaders = config.LogCaptureRequestHeaders.ValueBool()`,
		`loggingConfig.CaptureRequestBody = config.LogCaptureRequestBody.ValueBool()`,
		`loggingConfig.CaptureResponseHeaders = config.LogCaptureResponseHeaders.ValueBool()`,
		`loggingConfig.CaptureResponseBody = config.LogCaptureResponseBody.ValueBool()`,
		`loggingConfig.MaxBodyBytes = int(config.LogMaxBodyBytes.ValueInt64())`,
		`if loggingConfig.LogFile != "" {`,
		`opts = append(opts, client.WithLogging(loggingConfig))`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Configure missing %q\n--- file ---\n%s", want, got)
		}
	}
}

// TestConfigure_WithLoggingBakedDefaults_Render asserts the generator.yaml
// logging settings carried on ClientIR.Logging are baked into the
// client.LoggingConfig literal as defaults the practitioner attributes
// override.
func TestConfigure_WithLoggingBakedDefaults_Render(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	pir.ClientIR.Logging = &ir.LoggingIR{
		LogFile:            "provider.log",
		CaptureRequestBody: true,
		MaxBodyBytes:       8192,
		RedactHeaders:      []string{"Authorization", "X-API-Key"},
	}
	got := renderProvider(t, pir)

	for _, want := range []string{
		`LogFile: "provider.log"`,
		`CaptureRequestBody: true`,
		`MaxBodyBytes: 8192`,
		`RedactHeaders: []string{"Authorization", "X-API-Key"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Configure logging literal missing baked default %q\n--- file ---\n%s", want, got)
		}
	}
}

// TestConfigure_NoWiredClientOmitsWithLogging asserts a provider with no wired
// resources/data sources (no generated client in Configure) emits no logging
// wiring at all.
func TestConfigure_NoWiredClientOmitsWithLogging(t *testing.T) {
	pir := sampleProviderWithResourceIR()
	pir.Resources = nil
	got := renderProvider(t, pir)

	if strings.Contains(got, "client.WithLogging") {
		t.Errorf("unwired provider must not reference client.WithLogging\n--- file ---\n%s", got)
	}
	// The log_* schema attributes are still present; only the client wiring is
	// absent.
	if !strings.Contains(got, `"log_file": schema.StringAttribute{`) {
		t.Errorf("log_* schema attributes must be present even without a wired client\n--- file ---\n%s", got)
	}
}

// TestConfigure_WithLoggingCall_Compiles generates a full provider module with
// a wired resource and compiles it, proving the log_* schema attributes, the
// model fields, and the client.WithLogging wiring type-check against the
// generated client.
func TestConfigure_WithLoggingCall_Compiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network-bound compile test in -short mode")
	}
	p := sampleProviderWithResourceIR()
	p.ClientIR.Logging = &ir.LoggingIR{
		LogFile:       "provider.log",
		MaxBodyBytes:  8192,
		RedactHeaders: []string{"Authorization"},
	}
	tmp := generateResourceModule(t, p)

	ctx, cancel := contextWithTimeout(t, 5*time.Minute)
	defer cancel()

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = tmp
	if out, err := tidyCmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed: %v\n%s", err, out)
	}
	buildCmd := exec.CommandContext(ctx, "go", "build", "./...")
	buildCmd.Dir = tmp
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./... failed for provider with logging wiring: %v\n%s", err, out)
	}
}
