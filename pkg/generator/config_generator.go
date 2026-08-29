// Package generator emits Terraform provider source code, documentation, tests,
// and release artifacts.
package generator

import (
	"fmt"
	"io"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/signalbreak-labs/eidos/pkg/config"
	"github.com/signalbreak-labs/eidos/pkg/ir"
)

// GenerateConfig converts a ProviderIR into a generator.yaml Config.
//
// The returned Config covers every top-level field defined in
// PROJECT_DESIGN.md#14: provider, servers, security (auth), resources,
// data_sources, actions, ephemeral_resources, list_resources, polymorphism,
// timeouts, pagination, logging, and generate_terraform_tests.
func GenerateConfig(providerIR ir.ProviderIR) (*config.Config, error) {
	cfg := &config.Config{
		Provider: config.ProviderConfig{
			Name:            providerIR.Name,
			DisplayName:     providerIR.FullName,
			Version:         defaultVersion(providerIR.Version),
			Description:     providerIR.Description,
			ProtocolVersion: config.DefaultProtocolVersion,
		},
		Servers:                convertServers(providerIR.Servers),
		ResourceOverrides:      convertResources(providerIR),
		DatasourceOverrides:    convertDatasources(providerIR),
		ActionOverrides:        convertActions(providerIR.Actions),
		EphemeralOverrides:     convertEphemeralResources(providerIR.EphemeralResources),
		ListResourceOverrides:  convertListResources(providerIR.ListResources),
		FunctionOverrides:      convertFunctions(providerIR.Functions),
		Auth:                   convertAuth(providerIR.Name, providerIR.SecurityIR),
		Pagination:             convertPagination(providerIR.ClientIR.Pagination),
		GlobalTimeouts:         defaultGlobalTimeouts(),
		Logging:                convertLogging(providerIR.ClientIR.Logging),
		GenerateTerraformTests: boolPtr(providerIR.GenerateTerraformTests),
	}

	// Derive any polymorphism configuration from union schemas discovered in the IR.
	cfg.Polymorphism = convertPolymorphism(detectUnions(providerIR))

	// Detect auth schemes that sanitize to the same env-var suffix (e.g.
	// "api-key" and "api_key" both collapse to API_KEY). The collision is
	// otherwise silent — sanitizeEnvSuffix only log.Printfs — so surface it as
	// a config warning naming the schemes involved (L-31).
	cfg.Warnings = append(cfg.Warnings, warnEnvVarCollisions(providerIR.SecurityIR)...)

	config.ApplyDefaults(cfg)
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("generated config is invalid: %w", err)
	}

	return cfg, nil
}

// MarshalConfig serializes a ProviderIR into generator.yaml bytes.
func MarshalConfig(providerIR ir.ProviderIR) ([]byte, error) {
	cfg, err := GenerateConfig(providerIR)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}

// ConfigFile returns the generated generator.yaml file for a provider.
// If config generation fails, the returned File renders the error so that write
// mode surfaces it instead of silently omitting the file.
func ConfigFile(providerIR ir.ProviderIR) File {
	data, err := MarshalConfig(providerIR)
	if err != nil {
		return ErrorFile("generator.yaml", fmt.Errorf("failed to marshal generator.yaml: %w", err))
	}
	return File{
		Path: "generator.yaml",
		Render: func(w io.Writer) error {
			_, err := w.Write(data)
			return err
		},
	}
}

func defaultVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return config.DefaultProviderVersion
	}
	return v
}

func defaultGlobalTimeouts() *config.TimeoutConfig {
	return &config.TimeoutConfig{
		Create: durationPtr(config.Duration(20 * time.Minute)),
		Read:   durationPtr(config.Duration(10 * time.Minute)),
		Update: durationPtr(config.Duration(20 * time.Minute)),
		Delete: durationPtr(config.Duration(10 * time.Minute)),
	}
}

// convertLogging maps a LoggingIR back to the generator.yaml logging section.
// It is the exact inverse of the transformer's forward mapping (handler.go's
// buildLoggingIR: Enabled -> LogFile=FilePath, capture flags/max/redact copied):
// FilePath gets FilePath and Enabled is derived from a non-empty LogFile, so
// feeding the emitted `logging:` section back into a run reproduces the same
// ClientIR.Logging and the same generated LoggingConfig (N-68). When the IR has
// no logging configured (a run without a logging section), nil is returned so
// the canonical generator.yaml stays honest instead of declaring logging
// defaults the generated code never applied.
func convertLogging(l *ir.LoggingIR) *config.LoggingConfig {
	if l == nil {
		return nil
	}
	return &config.LoggingConfig{
		Enabled:                l.LogFile != "",
		FilePath:               l.LogFile,
		CaptureRequestHeaders:  l.CaptureRequestHeaders,
		CaptureRequestBody:     l.CaptureRequestBody,
		CaptureResponseHeaders: l.CaptureResponseHeaders,
		CaptureResponseBody:    l.CaptureResponseBody,
		MaxBodyBytes:           l.MaxBodyBytes,
		RedactHeaders:          l.RedactHeaders,
	}
}

func convertServers(servers []ir.ServerIR) []config.ServerConfig {
	out := make([]config.ServerConfig, 0, len(servers))
	for _, s := range servers {
		variables := make(map[string]config.ServerVariableConfig, len(s.Variables))
		for name, v := range s.Variables {
			variables[name] = config.ServerVariableConfig{
				Default:     v.Default,
				Enum:        v.Enum,
				Description: v.Description,
			}
		}
		out = append(out, config.ServerConfig{
			URL:         s.URL,
			Description: s.Description,
			Variables:   variables,
		})
	}
	return out
}

func convertResources(provider ir.ProviderIR) []config.ResourceOverride {
	resources := provider.Resources
	out := make([]config.ResourceOverride, 0, len(resources))
	for _, r := range resources {
		var timeouts *config.TimeoutConfig
		if r.Timeouts != nil {
			timeouts = convertTimeoutConfigIR(r.Timeouts)
		}

		schemaName := r.Name
		if schemaName == "" {
			schemaName = r.TypeName
		}

		writeOnly := extractWriteOnlyAttributes(r.Schema)
		computed := extractComputedAttributes(r.Schema)
		forceNew := extractForceNewAttributes(r.Schema)
		sensitive := mergeSensitiveAttrs(r.SensitiveAttrs, extractSensitiveAttributes(r.Schema))

		ro := config.ResourceOverride{
			Schema:              schemaName,
			Operation:           r.SourceOperation,
			ResourceName:        r.Name,
			IDAttribute:         r.IDAttribute,
			ImportFormat:        r.ImportIDFormat,
			Timeouts:            timeouts,
			ForceNew:            forceNew,
			ComputedAttributes:  computed,
			SensitiveAttributes: sensitive,
			WriteOnlyAttributes: writeOnly,
			Description:         r.Description,
		}
		// Override-created resources (promoted from a resource_override with
		// generate_resource/create_operation/...) are not reproducible from CRUD
		// inference alone. Re-emit generate_resource plus the CRUD operation ids
		// so a normalized generator.yaml round-trips instead of silently dropping
		// the resource on regeneration (G8). Inferred resources stay bare; the
		// grouping pass reproduces them without these fields.
		if r.OverrideCreated {
			ro.GenerateResource = boolPtr(true)
			ro.CreateOperation = r.CRUDMapping.Create.OperationID
			ro.ReadOperation = r.CRUDMapping.Read.OperationID
			if r.CRUDMapping.Update != nil {
				ro.UpdateOperation = r.CRUDMapping.Update.OperationID
			}
			ro.DeleteOperation = r.CRUDMapping.Delete.OperationID
		}
		// A data source emitted by the resource_overrides.generate_datasource
		// opt-in carries the resource's SourceOperation (not the read operation's)
		// as its marker. Re-emit generate_datasource + datasource_name so a
		// normalized generator.yaml round-trips instead of silently dropping the
		// data source on regeneration (M-17/G8).
		for _, ds := range provider.DataSources {
			if ds.SourceOperation != "" && ds.SourceOperation == r.SourceOperation {
				ro.GenerateDatasource = boolPtr(true)
				ro.DatasourceName = ds.Name
				break
			}
		}
		out = append(out, ro)
	}
	return out
}

func convertDatasources(provider ir.ProviderIR) []config.DatasourceOverride {
	// Skip data sources emitted by the generate_datasource opt-in: they carry the
	// resource's SourceOperation as their marker, and convertResources re-emits
	// generate_datasource for them. Emitting a DatasourceOverride here too would
	// key it on the resource's create operation — semantically wrong for a data
	// source override — and duplicate the data source on regeneration (M-17/G8).
	resourceDerived := make(map[string]bool)
	for _, r := range provider.Resources {
		if r.SourceOperation != "" {
			resourceDerived[r.SourceOperation] = true
		}
	}
	ds := provider.DataSources
	out := make([]config.DatasourceOverride, 0, len(ds))
	for _, d := range ds {
		if d.SourceOperation != "" && resourceDerived[d.SourceOperation] {
			continue
		}
		// Match the transformer's override resolution, which keys data source
		// overrides against SourceOperation (transformer/override.go). Every
		// sibling converter uses SourceOperation with a Name fallback; using
		// Name directly here produced generator.yaml files that failed to
		// match overrides whenever Name != SourceOperation (M-12).
		op := d.SourceOperation
		if op == "" {
			op = d.Name
		}
		out = append(out, config.DatasourceOverride{
			Operation:      op,
			DatasourceName: d.Name,
		})
	}
	return out
}

func convertActions(actions []ir.ActionIR) []config.ActionOverride {
	out := make([]config.ActionOverride, 0, len(actions))
	for _, a := range actions {
		op := a.SourceOperation
		if op == "" {
			op = a.Name
		}
		out = append(out, config.ActionOverride{
			Operation:        op,
			Name:             a.Name,
			Description:      a.Description,
			ProgressMessages: a.ProgressMessages,
			ModifyPlan:       a.ModifyPlan,
		})
	}
	return out
}

func convertEphemeralResources(ephemerals []ir.EphemeralResourceIR) []config.EphemeralOverride {
	out := make([]config.EphemeralOverride, 0, len(ephemerals))
	for _, e := range ephemerals {
		op := e.SourceOperation
		if op == "" {
			op = e.Name
		}
		out = append(out, config.EphemeralOverride{
			Operation:    op,
			Name:         e.Name,
			Description:  e.Description,
			OpenMapping:  mappingString(e.OpenMapping),
			CloseMapping: mappingStringPtr(e.CloseMapping),
			RenewMapping: mappingStringPtr(e.RenewMapping),
			ResultFields: convertResultFields(e.ResultSchema),
		})
	}
	return out
}

func convertListResources(lists []ir.ListResourceIR) []config.ListResourceOverride {
	out := make([]config.ListResourceOverride, 0, len(lists))
	for _, l := range lists {
		resourceName := l.Name
		if resourceName == "" {
			resourceName = l.TypeName
		}
		op := l.SourceOperation
		if op == "" {
			op = l.Name
		}

		var pagination *config.PaginationConfig
		if l.PaginationStyle != "" {
			pagination = &config.PaginationConfig{Style: l.PaginationStyle}
		}

		out = append(out, config.ListResourceOverride{
			Resource:     resourceName,
			Operation:    op,
			ConfigSchema: convertListConfigSchema(l.ConfigSchema),
			Pagination:   pagination,
		})
	}
	return out
}

func convertFunctions(functions []ir.FunctionIR) []config.FunctionOverride {
	out := make([]config.FunctionOverride, 0, len(functions))
	for _, f := range functions {
		op := f.SourceOperation
		if op == "" {
			op = f.Name
		}
		out = append(out, config.FunctionOverride{
			Operation:  op,
			Name:       f.Name,
			Type:       f.TypeName,
			Arguments:  convertFunctionArguments(f.Arguments),
			ReturnType: schemaTypeString(f.ReturnType),
		})
	}
	return out
}

func convertAuth(providerName string, sec ir.SecurityIR) []config.AuthConfig {
	out := make([]config.AuthConfig, 0, len(sec.Schemes))
	for _, scheme := range sec.Schemes {
		out = append(out, convertSecurityScheme(providerName, scheme))
	}
	return out
}

// warnEnvVarCollisions returns warning strings for auth scheme names that
// sanitize to the same env-var suffix. sanitizeEnvSuffix collapses runs of
// non-[A-Za-z0-9_] characters into a single underscore, so distinct scheme
// names such as "api-key" and "api_key" both produce the suffix "API_KEY" and
// therefore the same env var. The collision is silent in sanitizeEnvSuffix
// (only a log.Printf), so it is surfaced here as a config warning naming the
// colliding scheme names (L-31).
func warnEnvVarCollisions(sec ir.SecurityIR) []string {
	suffixToSchemes := make(map[string][]string)
	for _, scheme := range sec.Schemes {
		suffix := envSuffix(scheme.Name)
		if suffix == "" || suffix == "UNKNOWN" {
			continue
		}
		suffixToSchemes[suffix] = append(suffixToSchemes[suffix], scheme.Name)
	}
	var warnings []string
	for suffix, names := range suffixToSchemes {
		if len(names) < 2 {
			continue
		}
		// Sort for deterministic warning output.
		sort.Strings(names)
		warnings = append(warnings, fmt.Sprintf(
			"auth schemes %s all sanitize to the same env-var suffix %q; rename the schemes or set explicit env vars to avoid one overwriting another",
			strings.Join(quoteEach(names), ", "), suffix,
		))
	}
	sort.Strings(warnings)
	return warnings
}

// quoteEach returns a copy of ss with each element double-quoted.
func quoteEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return out
}

func convertSecurityScheme(providerName string, scheme ir.SecuritySchemeIR) config.AuthConfig {
	suffix := envSuffix(scheme.Name)
	switch scheme.Type {
	case ir.SecuritySchemeAPIKey:
		ac := config.AuthConfig{
			Scheme: "apiKey",
			// The apiKey value is always sourced from an env var regardless of
			// placement; the generated provider's APIKeyAuth interceptor injects
			// it as a header, query, or cookie per the spec's `in` (see
			// pkg/generator/provider_auth.go). HeaderName is set only for header
			// placement — query/cookie apiKeys carry no header, and EnvVar alone
			// satisfies ValidateAuth ("apiKey requires header_name or env_var").
			EnvVar: scheme.EnvVar,
		}
		// A user-customized env var from generator.yaml `auth:` is preserved
		// through the round-trip; otherwise recompute from the provider prefix
		// (M-5).
		if ac.EnvVar == "" {
			ac.EnvVar = fmt.Sprintf("%s_%s", envPrefix(providerName), suffix)
		}
		if scheme.In == "header" || scheme.In == "" {
			ac.HeaderName = scheme.NameField
		}
		return ac
	case ir.SecuritySchemeHTTP:
		switch strings.ToLower(scheme.Scheme) {
		case "basic":
			return config.AuthConfig{Scheme: "basic"}
		case "bearer":
			ac := config.AuthConfig{Scheme: "bearer", EnvVar: scheme.EnvVar}
			if ac.EnvVar == "" {
				ac.EnvVar = fmt.Sprintf("%s_%s", envPrefix(providerName), suffix)
			}
			return ac
		default:
			// Treat unknown HTTP schemes as bearer with an env hint.
			ac := config.AuthConfig{Scheme: "bearer", EnvVar: scheme.EnvVar}
			if ac.EnvVar == "" {
				ac.EnvVar = fmt.Sprintf("%s_%s", envPrefix(providerName), suffix)
			}
			return ac
		}
	case ir.SecuritySchemeOAuth2:
		ac := config.AuthConfig{Scheme: "oauth2"}
		ac.Flow = oauth2Flow(scheme.Flows)
		// A generator.yaml `auth:` flow selection overrides the derived flow so
		// the round-trip preserves the user's choice (M-5).
		if scheme.SelectedFlow != "" {
			ac.Flow = scheme.SelectedFlow
		}
		ac.TokenURL = oauth2TokenURL(scheme)
		ac.ClientIDEnv = scheme.ClientIDEnv
		if ac.ClientIDEnv == "" {
			ac.ClientIDEnv = fmt.Sprintf("%s_CLIENT_ID", envPrefix(providerName))
		}
		ac.ClientSecretEnv = scheme.ClientSecretEnv
		if ac.ClientSecretEnv == "" {
			ac.ClientSecretEnv = fmt.Sprintf("%s_CLIENT_SECRET", envPrefix(providerName))
		}
		return ac
	case ir.SecuritySchemeOpenIDConnect:
		// OpenID Connect discovery URLs are not token endpoints; route them to
		// DiscoveryURL so downstream generators do not conflate the two.
		ac := config.AuthConfig{
			Scheme:       "oauth2",
			Flow:         "openIdConnect",
			DiscoveryURL: scheme.OpenIDConnectURL,
			ClientIDEnv:  scheme.ClientIDEnv,
		}
		if ac.ClientIDEnv == "" {
			ac.ClientIDEnv = fmt.Sprintf("%s_CLIENT_ID", envPrefix(providerName))
		}
		return ac
	default:
		return config.AuthConfig{
			Scheme: "apiKey",
			EnvVar: fmt.Sprintf("%s_%s", envPrefix(providerName), suffix),
		}
	}
}

// oauth2TokenURL returns the token URL of the OAuth2 flow the generated provider
// will use: the flow named by the scheme's SelectedFlow override when set,
// otherwise the priority-order first declared flow. A generator.yaml `auth:`
// token_url override is applied to that flow by ApplyAuthOverrides, so reading
// the active flow here preserves the override through the round-trip (M-5).
func oauth2TokenURL(scheme ir.SecuritySchemeIR) string {
	if scheme.Flows == nil {
		return ""
	}
	switch scheme.SelectedFlow {
	case "client_credentials":
		if scheme.Flows.ClientCredentials != nil {
			return scheme.Flows.ClientCredentials.TokenURL
		}
	case "password":
		if scheme.Flows.Password != nil {
			return scheme.Flows.Password.TokenURL
		}
	case "authorization_code":
		if scheme.Flows.AuthorizationCode != nil {
			return scheme.Flows.AuthorizationCode.TokenURL
		}
	case "implicit":
		if scheme.Flows.Implicit != nil {
			return scheme.Flows.Implicit.TokenURL
		}
	}
	switch {
	case scheme.Flows.ClientCredentials != nil:
		return scheme.Flows.ClientCredentials.TokenURL
	case scheme.Flows.Password != nil:
		return scheme.Flows.Password.TokenURL
	case scheme.Flows.AuthorizationCode != nil:
		return scheme.Flows.AuthorizationCode.TokenURL
	}
	return ""
}

func oauth2Flow(flows *ir.OAuthFlowsIR) string {
	if flows == nil {
		return "client_credentials"
	}
	switch {
	case flows.ClientCredentials != nil:
		return "client_credentials"
	case flows.AuthorizationCode != nil:
		return "authorization_code"
	case flows.Password != nil:
		return "password"
	case flows.Implicit != nil:
		return "implicit"
	default:
		return "client_credentials"
	}
}

func convertPagination(pag *ir.PaginationIR) *config.PaginationConfig {
	if pag == nil || pag.Style == "" {
		return nil
	}
	return &config.PaginationConfig{
		Style:            pag.Style,
		PageParam:        pag.PageParam,
		PerPageParam:     pag.PerPageParam,
		TotalCountHeader: pag.TotalCountHeader,
		NextLinkHeader:   pag.NextLinkHeader,
		CursorField:      pag.CursorField,
	}
}

func convertTimeoutConfigIR(t *ir.TimeoutConfigIR) *config.TimeoutConfig {
	if t == nil {
		return nil
	}
	out := &config.TimeoutConfig{}
	hasAny := false
	if t.Create != nil {
		out.Create = durationPtr(config.Duration(*t.Create))
		hasAny = true
	}
	if t.Read != nil {
		out.Read = durationPtr(config.Duration(*t.Read))
		hasAny = true
	}
	if t.Update != nil {
		out.Update = durationPtr(config.Duration(*t.Update))
		hasAny = true
	}
	if t.Delete != nil {
		out.Delete = durationPtr(config.Duration(*t.Delete))
		hasAny = true
	}
	if !hasAny {
		return nil
	}
	return out
}

// convertListConfigSchema converts list-resource filter/search arguments. An
// attribute is marked Optional when it is explicitly optional or when it is not
// explicitly required; this treats unset optionality as optional for list filter
// parameters, which is the expected default for search arguments.
func convertListConfigSchema(schema ir.ObjectSchemaIR) []config.ListConfigSchema {
	out := make([]config.ListConfigSchema, 0, len(schema.Attributes))
	for _, attr := range schema.Attributes {
		optional := attr.Optional || !attr.Required
		out = append(out, config.ListConfigSchema{
			Name:        attr.Name,
			Type:        schemaTypeString(attr.Schema),
			Optional:    &optional,
			Description: attr.Description,
		})
	}
	return out
}

func convertResultFields(schema ir.ObjectSchemaIR) []config.ResultField {
	out := make([]config.ResultField, 0, len(schema.Attributes))
	for _, attr := range schema.Attributes {
		out = append(out, config.ResultField{
			Name:      attr.Name,
			Type:      schemaTypeString(attr.Schema),
			Sensitive: attr.Sensitive,
		})
	}
	return out
}

func convertFunctionArguments(args []ir.FunctionParamIR) []config.FunctionArgument {
	out := make([]config.FunctionArgument, 0, len(args))
	for _, a := range args {
		out = append(out, config.FunctionArgument{
			Name: a.Name,
			Type: schemaTypeString(a.Schema),
		})
	}
	return out
}

func schemaTypeString(s ir.SchemaIR) string {
	if s.Type != "" {
		return string(s.Type)
	}
	if s.Collection != nil {
		return fmt.Sprintf("%s(%s)", s.Collection.Kind, schemaTypeString(s.Collection.ElementType))
	}
	if s.Union != nil {
		return "dynamic"
	}
	if len(s.Attributes) > 0 || len(s.Blocks) > 0 {
		return "object"
	}
	return "string"
}

func mappingString(m ir.OperationMappingIR) string {
	if m.Method == "" && m.PathTemplate == "" {
		return ""
	}
	return fmt.Sprintf("%s %s", m.Method, m.PathTemplate)
}

func mappingStringPtr(m *ir.OperationMappingIR) string {
	if m == nil {
		return ""
	}
	return mappingString(*m)
}

func extractWriteOnlyAttributes(schema ir.ObjectSchemaIR) []config.WriteOnlyAttribute {
	var attrs []config.WriteOnlyAttribute
	// Dedupe by the full dotted path, not the bare leaf name: two attributes
	// named "name" at different nesting levels are distinct write-only arguments
	// and must not collapse into a single config entry (L-30).
	seen := make(map[string]struct{})
	walkObjectSchemaWithPath(schema, func(attr ir.AttributeIR, path string) {
		// AttributeIR is the single source of truth for attribute flags; the
		// embedded SchemaIR no longer duplicates them (N-49).
		if attr.WriteOnly {
			if _, ok := seen[path]; !ok {
				attrs = append(attrs, config.WriteOnlyAttribute{
					Name:        attr.Name,
					Path:        path,
					Description: attr.Description,
					Sensitive:   attr.Sensitive,
				})
				seen[path] = struct{}{}
			}
		}
	})
	return attrs
}

// extractComputedAttributes returns the leaf names of computed attributes.
// Dedup is intentionally by bare leaf name rather than dotted path: the consumer
// (transformer.setAttributeFlag) walks the whole schema tree and applies the
// flag to every attribute whose name matches, so a single "name" entry
// correctly covers every same-named computed attribute regardless of nesting
// level. Path qualification here would break that global-match semantics.
//
// A leaf name that is Required anywhere is excluded: setAttributeFlag clears
// Required when it applies "computed", so emitting such a name would downgrade
// a required create-body field to read-only and make the resource uncreatable.
// This happens when a nested computed attribute shares a leaf name with a
// top-level create-body field (e.g. SpaceTraders ships: the create body sends
// waypointSymbol, the read response carries nav.waypointSymbol; both normalize
// to "waypoint_symbol"). The schema inference already marks the nested
// attribute Computed, so omitting it from the override changes nothing for the
// nested attribute while preserving the writable top-level field.
//
// A leaf name whose value the CRUD body sends to the API (RequestInput) is
// likewise excluded, even when Optional: emitting it would (on regeneration)
// mark a spec-settable request field read-only, leaving the request sending a
// value the practitioner can never supply — the self-inflicted shape of the
// GigaVUE-FM clusterId query-parameter demotion (G39).
func extractComputedAttributes(schema ir.ObjectSchemaIR) []string {
	required := make(map[string]struct{})
	requestInput := make(map[string]struct{})
	walkObjectSchema(schema, func(attr ir.AttributeIR) {
		if attr.Required {
			required[attr.Name] = struct{}{}
		}
		if attr.RequestInput {
			requestInput[attr.Name] = struct{}{}
		}
	})
	var attrs []string
	seen := make(map[string]struct{})
	walkObjectSchema(schema, func(attr ir.AttributeIR) {
		// AttributeIR is the single source of truth for attribute flags (N-49).
		if !attr.Computed {
			return
		}
		if _, isRequired := required[attr.Name]; isRequired {
			return
		}
		if _, isInput := requestInput[attr.Name]; isInput {
			return
		}
		if _, ok := seen[attr.Name]; !ok {
			attrs = append(attrs, attr.Name)
			seen[attr.Name] = struct{}{}
		}
	})
	return attrs
}

// extractSensitiveAttributes returns the leaf names of sensitive attributes.
// See extractComputedAttributes for why dedup is by bare leaf name.
func extractSensitiveAttributes(schema ir.ObjectSchemaIR) []string {
	var attrs []string
	seen := make(map[string]struct{})
	walkObjectSchema(schema, func(attr ir.AttributeIR) {
		// AttributeIR is the single source of truth for attribute flags (N-49).
		if attr.Sensitive {
			if _, ok := seen[attr.Name]; !ok {
				attrs = append(attrs, attr.Name)
				seen[attr.Name] = struct{}{}
			}
		}
	})
	return attrs
}

func hasRequiresReplace(pms []ir.PlanModifierIR) bool {
	for _, pm := range pms {
		if pm.Type == ir.PlanModifierTypeRequiresReplace {
			return true
		}
	}
	return false
}

// extractForceNewAttributes returns the leaf names of attributes that force
// replacement. See extractComputedAttributes for why dedup is by bare leaf
// name.
func extractForceNewAttributes(schema ir.ObjectSchemaIR) []string {
	var attrs []string
	seen := make(map[string]struct{})
	walkObjectSchema(schema, func(attr ir.AttributeIR) {
		// AttributeIR is the single source of truth for attribute flags (N-49).
		if hasRequiresReplace(attr.PlanModifiers) {
			if _, ok := seen[attr.Name]; !ok {
				attrs = append(attrs, attr.Name)
				seen[attr.Name] = struct{}{}
			}
		}
	})
	return attrs
}

func mergeSensitiveAttrs(explicit, inferred []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(explicit)+len(inferred))
	for _, name := range explicit {
		if _, ok := seen[name]; !ok {
			out = append(out, name)
			seen[name] = struct{}{}
		}
	}
	for _, name := range inferred {
		if _, ok := seen[name]; !ok {
			out = append(out, name)
			seen[name] = struct{}{}
		}
	}
	return out
}

func walkObjectSchema(schema ir.ObjectSchemaIR, fn func(ir.AttributeIR)) {
	walkObjectSchemaNode(schema, nil, fn)
}

// walkObjectSchemaWithPath visits every attribute in the schema tree, passing
// the dotted path of each attribute (e.g. "owner.password"). The root-level
// attributes have a path equal to their own name. It is the path-aware
// counterpart to walkObjectSchema for callers that need to distinguish
// same-named attributes at different nesting levels (L-30).
func walkObjectSchemaWithPath(schema ir.ObjectSchemaIR, fn func(attr ir.AttributeIR, path string)) {
	walkSchemaNodeWithPath(ir.SchemaIR{Attributes: schema.Attributes, Blocks: schema.Blocks}, "", fn)
}

// walkSchemaNodeWithPath is the recursive driver for walkObjectSchemaWithPath.
// parent is the dotted path of the enclosing attribute or block.
func walkSchemaNodeWithPath(schema ir.SchemaIR, parent string, fn func(attr ir.AttributeIR, path string)) {
	for _, attr := range schema.Attributes {
		path := attr.Name
		if parent != "" {
			path = parent + "." + attr.Name
		}
		fn(attr, path)
		walkSchemaNodeWithPath(attr.Schema, path, fn)
	}
	for _, block := range schema.Blocks {
		child := block.Name
		if parent != "" {
			child = parent + "." + block.Name
		}
		walkSchemaNodeWithPath(ir.SchemaIR{Attributes: block.Schema.Attributes, Blocks: block.Schema.Blocks}, child, fn)
	}
	if schema.Collection != nil {
		// Nested attributes inside a collection element keep the parent path;
		// the element itself is not a named attribute.
		walkSchemaNodeWithPath(schema.Collection.ElementType, parent, fn)
	}
	if schema.Union != nil {
		for _, variant := range schema.Union.Variants {
			walkSchemaNodeWithPath(variant, parent, fn)
		}
	}
}

// walkSchemaNode recursively visits every SchemaIR node in a schema tree. The
// optional schemaFn callback is invoked for each node before recursing; the
// optional attrFn callback is invoked for each attribute encountered. Either
// callback may be nil, in which case that phase of the walk is skipped. This
// partial contract is intentional: callers such as detectUnions only need the
// schema visitor, while the extract* helpers only need the attribute visitor.
func walkSchemaNode(schema ir.SchemaIR, schemaFn func(ir.SchemaIR), attrFn func(ir.AttributeIR)) {
	if schemaFn != nil {
		schemaFn(schema)
	}
	for _, attr := range schema.Attributes {
		if attrFn != nil {
			attrFn(attr)
		}
		walkSchemaNode(attr.Schema, schemaFn, attrFn)
	}
	for _, block := range schema.Blocks {
		walkObjectSchemaNode(block.Schema, schemaFn, attrFn)
	}
	if schema.Collection != nil {
		walkSchemaNode(schema.Collection.ElementType, schemaFn, attrFn)
	}
	if schema.Union != nil {
		for _, variant := range schema.Union.Variants {
			walkSchemaNode(variant, schemaFn, attrFn)
		}
	}
}

// walkObjectSchemaNode is the object-level entry point for walkSchemaNode.
func walkObjectSchemaNode(schema ir.ObjectSchemaIR, schemaFn func(ir.SchemaIR), attrFn func(ir.AttributeIR)) {
	for _, attr := range schema.Attributes {
		if attrFn != nil {
			attrFn(attr)
		}
		walkSchemaNode(attr.Schema, schemaFn, attrFn)
	}
	for _, block := range schema.Blocks {
		walkObjectSchemaNode(block.Schema, schemaFn, attrFn)
	}
}

// unionDetection captures a single oneOf-style union discovered while walking IR.
type unionDetection struct {
	Schema        string
	Variants      []ir.SchemaIR
	Discriminator *ir.DiscriminatorIR
}

func detectUnions(providerIR ir.ProviderIR) []unionDetection {
	var detections []unionDetection
	seen := make(map[*ir.UnionType]struct{})

	collect := func(s ir.SchemaIR) {
		if s.Union == nil {
			return
		}
		if _, ok := seen[s.Union]; ok {
			return
		}
		seen[s.Union] = struct{}{}
		detections = append(detections, unionDetection{
			Schema:        s.Name,
			Variants:      s.Union.Variants,
			Discriminator: s.Union.Discriminator,
		})
	}

	for _, r := range providerIR.Resources {
		walkObjectSchemaNode(r.Schema, collect, nil)
	}
	for _, d := range providerIR.DataSources {
		walkObjectSchemaNode(d.Schema, collect, nil)
	}
	for _, a := range providerIR.Actions {
		walkObjectSchemaNode(a.ConfigSchema, collect, nil)
	}
	for _, e := range providerIR.EphemeralResources {
		walkObjectSchemaNode(e.ConfigSchema, collect, nil)
		walkObjectSchemaNode(e.ResultSchema, collect, nil)
	}
	for _, l := range providerIR.ListResources {
		walkObjectSchemaNode(l.ConfigSchema, collect, nil)
		walkObjectSchemaNode(l.IdentitySchema, collect, nil)
		if l.ResourceSchema != nil {
			walkObjectSchemaNode(*l.ResourceSchema, collect, nil)
		}
	}
	for _, f := range providerIR.Functions {
		for _, arg := range f.Arguments {
			walkSchemaNode(arg.Schema, collect, nil)
		}
		walkSchemaNode(f.ReturnType, collect, nil)
	}

	return detections
}

func convertPolymorphism(detections []unionDetection) *config.PolymorphismConfig {
	if len(detections) == 0 {
		return nil
	}
	var overrides []config.OneOfOverride
	for _, d := range detections {
		if strings.TrimSpace(d.Schema) == "" {
			continue
		}
		variants := make([]config.Variant, 0, len(d.Variants))
		for _, v := range d.Variants {
			if strings.TrimSpace(v.Name) == "" {
				continue
			}
			var vdisc *config.DiscriminatorConfig
			if d.Discriminator != nil {
				if mapped, ok := d.Discriminator.Mapping[v.Name]; ok {
					// Each variant carries only its own discriminator mapping entry.
					// This is intentional: the per-variant mapping is used to identify
					// the variant, not the whole union mapping. The cost is O(V) map
					// allocations; do not refactor to a shared map without changing
					// the semantics.
					vdisc = &config.DiscriminatorConfig{
						PropertyName: d.Discriminator.PropertyName,
						Mapping:      map[string]string{mapped: v.Name},
					}
				}
			}
			variants = append(variants, config.Variant{
				Schema:        v.Name,
				Discriminator: vdisc,
			})
		}
		if len(variants) == 0 {
			continue
		}
		var odisc *config.DiscriminatorConfig
		if d.Discriminator != nil {
			odisc = &config.DiscriminatorConfig{
				PropertyName: d.Discriminator.PropertyName,
				Mapping:      d.Discriminator.Mapping,
			}
		}
		overrides = append(overrides, config.OneOfOverride{
			Schema:        d.Schema,
			Variants:      variants,
			Discriminator: odisc,
		})
	}
	if len(overrides) == 0 {
		return nil
	}
	return &config.PolymorphismConfig{
		Strategy: "dynamic_union",
		OneOf:    overrides,
	}
}

var envSanitizer = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// maxEnvSuffixLength caps generated env-var suffixes to keep worst-case names
// within practical shell limits (e.g., ARG_MAX) while remaining readable.
const maxEnvSuffixLength = 64

func sanitizeEnvSuffix(name string) string {
	// Replace any run that is not an ASCII letter, digit, or underscore with a
	// single underscore, then trim leading/trailing underscores so the suffix
	// remains a valid shell identifier fragment.
	s := envSanitizer.ReplaceAllString(name, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		log.Printf("sanitizeEnvSuffix: input %q sanitized to empty string, falling back to UNKNOWN", name)
		return "UNKNOWN"
	}
	if len(s) > maxEnvSuffixLength {
		log.Printf("sanitizeEnvSuffix: input %q produced suffix longer than %d characters; truncating %q...", name, maxEnvSuffixLength, s[:maxEnvSuffixLength])
		s = s[:maxEnvSuffixLength]
	}
	return strings.ToUpper(s)
}

func envPrefix(providerName string) string {
	return sanitizeEnvSuffix(providerName)
}

func envSuffix(name string) string {
	return sanitizeEnvSuffix(name)
}

func boolPtr(b bool) *bool {
	return &b
}

func durationPtr(d config.Duration) *config.Duration {
	return &d
}
