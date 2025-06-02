// Package versioning provides API versioning strategies for backward compatibility
package versioning

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/LerianStudio/gomcp-sdk/protocol"
)

// APIVersion represents an API version
type APIVersion struct {
	Major int
	Minor int
	Patch int
}

// String returns the string representation of the version
func (v APIVersion) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Short returns the short version string (vMajor.Minor)
func (v APIVersion) Short() string {
	return fmt.Sprintf("v%d.%d", v.Major, v.Minor)
}

// IsCompatibleWith checks if this version is compatible with another version
func (v APIVersion) IsCompatibleWith(other APIVersion) bool {
	// Major version must match for compatibility
	if v.Major != other.Major {
		return false
	}

	// Minor version can be higher (backward compatible)
	return v.Minor >= other.Minor
}

// Compare compares two versions (-1: less, 0: equal, 1: greater)
func (v APIVersion) Compare(other APIVersion) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}

	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}

	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}

	return 0
}

// VersioningStrategy defines how API versioning is handled
type VersioningStrategy string

const (
	// VersioningStrategyHeader uses the API-Version header
	VersioningStrategyHeader VersioningStrategy = "header"

	// VersioningStrategyPath uses URL path versioning (/v1/tools)
	VersioningStrategyPath VersioningStrategy = "path"

	// VersioningStrategyQuery uses query parameter versioning (?version=v1.0)
	VersioningStrategyQuery VersioningStrategy = "query"

	// VersioningStrategyContentType uses content-type versioning (application/vnd.mcp.v1+json)
	VersioningStrategyContentType VersioningStrategy = "content-type"
)

// VersioningConfig configures API versioning behavior
type VersioningConfig struct {
	// Strategy for versioning
	Strategy VersioningStrategy

	// Default version to use when none specified
	DefaultVersion APIVersion

	// Supported versions
	SupportedVersions []APIVersion

	// Deprecated versions (still supported but warn clients)
	DeprecatedVersions []APIVersion

	// Header name for header-based versioning (default: "API-Version")
	HeaderName string

	// Query parameter name for query-based versioning (default: "version")
	QueryParam string

	// Content-Type vendor prefix for content-type versioning (default: "application/vnd.mcp")
	ContentTypeVendor string

	// Whether to include version info in response headers
	IncludeVersionHeaders bool

	// Whether to enforce strict versioning (reject unsupported versions)
	StrictVersioning bool
}

// DefaultVersioningConfig returns a default versioning configuration
func DefaultVersioningConfig() *VersioningConfig {
	return &VersioningConfig{
		Strategy:       VersioningStrategyHeader,
		DefaultVersion: APIVersion{Major: 1, Minor: 0, Patch: 0},
		SupportedVersions: []APIVersion{
			{Major: 1, Minor: 0, Patch: 0},
			{Major: 1, Minor: 1, Patch: 0},
		},
		DeprecatedVersions:    []APIVersion{},
		HeaderName:            "API-Version",
		QueryParam:            "version",
		ContentTypeVendor:     "application/vnd.mcp",
		IncludeVersionHeaders: true,
		StrictVersioning:      false,
	}
}

// VersionManager handles API versioning
type VersionManager struct {
	config *VersioningConfig
}

// NewVersionManager creates a new version manager
func NewVersionManager(config *VersioningConfig) *VersionManager {
	if config == nil {
		config = DefaultVersioningConfig()
	}

	return &VersionManager{
		config: config,
	}
}

// ExtractVersion extracts the API version from an HTTP request
func (vm *VersionManager) ExtractVersion(r *http.Request) (APIVersion, error) {
	switch vm.config.Strategy {
	case VersioningStrategyHeader:
		return vm.extractVersionFromHeader(r)
	case VersioningStrategyPath:
		return vm.extractVersionFromPath(r)
	case VersioningStrategyQuery:
		return vm.extractVersionFromQuery(r)
	case VersioningStrategyContentType:
		return vm.extractVersionFromContentType(r)
	default:
		return vm.config.DefaultVersion, nil
	}
}

// extractVersionFromHeader extracts version from HTTP header
func (vm *VersionManager) extractVersionFromHeader(r *http.Request) (APIVersion, error) {
	versionStr := r.Header.Get(vm.config.HeaderName)
	if versionStr == "" {
		return vm.config.DefaultVersion, nil
	}

	return parseVersion(versionStr)
}

// extractVersionFromPath extracts version from URL path
func (vm *VersionManager) extractVersionFromPath(r *http.Request) (APIVersion, error) {
	// Extract version from path like /v1/tools or /v1.1/tools
	re := regexp.MustCompile(`^/v(\d+)(?:\.(\d+))?(?:\.(\d+))?/`)
	matches := re.FindStringSubmatch(r.URL.Path)

	if len(matches) < 2 {
		return vm.config.DefaultVersion, nil
	}

	major, _ := strconv.Atoi(matches[1])
	minor := 0
	patch := 0

	if len(matches) > 2 && matches[2] != "" {
		minor, _ = strconv.Atoi(matches[2])
	}

	if len(matches) > 3 && matches[3] != "" {
		patch, _ = strconv.Atoi(matches[3])
	}

	return APIVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// extractVersionFromQuery extracts version from query parameter
func (vm *VersionManager) extractVersionFromQuery(r *http.Request) (APIVersion, error) {
	versionStr := r.URL.Query().Get(vm.config.QueryParam)
	if versionStr == "" {
		return vm.config.DefaultVersion, nil
	}

	return parseVersion(versionStr)
}

// extractVersionFromContentType extracts version from Content-Type header
func (vm *VersionManager) extractVersionFromContentType(r *http.Request) (APIVersion, error) {
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		return vm.config.DefaultVersion, nil
	}

	// Parse content type like "application/vnd.mcp.v1.0+json"
	re := regexp.MustCompile(`application/vnd\.mcp\.v(\d+)(?:\.(\d+))?(?:\.(\d+))?\+json`)
	matches := re.FindStringSubmatch(contentType)

	if len(matches) < 2 {
		return vm.config.DefaultVersion, nil
	}

	major, _ := strconv.Atoi(matches[1])
	minor := 0
	patch := 0

	if len(matches) > 2 && matches[2] != "" {
		minor, _ = strconv.Atoi(matches[2])
	}

	if len(matches) > 3 && matches[3] != "" {
		patch, _ = strconv.Atoi(matches[3])
	}

	return APIVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// ValidateVersion checks if a version is supported
func (vm *VersionManager) ValidateVersion(version APIVersion) error {
	// Check if version is supported
	for _, supported := range vm.config.SupportedVersions {
		if version.IsCompatibleWith(supported) {
			return nil
		}
	}

	if vm.config.StrictVersioning {
		return fmt.Errorf("unsupported API version: %s", version.String())
	}

	// In non-strict mode, allow but warn
	return nil
}

// IsDeprecated checks if a version is deprecated
func (vm *VersionManager) IsDeprecated(version APIVersion) bool {
	for _, deprecated := range vm.config.DeprecatedVersions {
		if version.Compare(deprecated) == 0 {
			return true
		}
	}
	return false
}

// SetVersionHeaders sets version-related headers in the response
func (vm *VersionManager) SetVersionHeaders(w http.ResponseWriter, version APIVersion) {
	if !vm.config.IncludeVersionHeaders {
		return
	}

	w.Header().Set("API-Version", version.String())
	w.Header().Set("API-Supported-Versions", vm.getSupportedVersionsString())

	if vm.IsDeprecated(version) {
		w.Header().Set("API-Deprecation-Warning", fmt.Sprintf("Version %s is deprecated", version.String()))
	}
}

// getSupportedVersionsString returns a comma-separated list of supported versions
func (vm *VersionManager) getSupportedVersionsString() string {
	versions := make([]string, len(vm.config.SupportedVersions))
	for i, v := range vm.config.SupportedVersions {
		versions[i] = v.String()
	}
	return strings.Join(versions, ", ")
}

// CreateVersioningMiddleware creates HTTP middleware for versioning
func (vm *VersionManager) CreateVersioningMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract API version
			version, err := vm.ExtractVersion(r)
			if err != nil {
				http.Error(w, fmt.Sprintf("Invalid API version: %v", err), http.StatusBadRequest)
				return
			}

			// Validate version
			if err := vm.ValidateVersion(version); err != nil {
				http.Error(w, err.Error(), http.StatusNotAcceptable)
				return
			}

			// Set version in context
			ctx := context.WithValue(r.Context(), "api_version", version)
			r = r.WithContext(ctx)

			// Set response headers
			vm.SetVersionHeaders(w, version)

			// Call next handler
			next.ServeHTTP(w, r)
		})
	}
}

// GetVersionFromContext extracts API version from context
func GetVersionFromContext(ctx context.Context) (APIVersion, bool) {
	version, ok := ctx.Value("api_version").(APIVersion)
	return version, ok
}

// VersionedHandler represents a handler for a specific API version
type VersionedHandler struct {
	Version APIVersion
	Handler http.Handler
}

// VersionRouter routes requests to version-specific handlers
type VersionRouter struct {
	versionManager *VersionManager
	handlers       map[string]*VersionedHandler
	defaultHandler http.Handler
}

// NewVersionRouter creates a new version router
func NewVersionRouter(versionManager *VersionManager) *VersionRouter {
	return &VersionRouter{
		versionManager: versionManager,
		handlers:       make(map[string]*VersionedHandler),
	}
}

// AddHandler adds a version-specific handler
func (vr *VersionRouter) AddHandler(version APIVersion, handler http.Handler) {
	vr.handlers[version.String()] = &VersionedHandler{
		Version: version,
		Handler: handler,
	}
}

// SetDefaultHandler sets the default handler for unmatched versions
func (vr *VersionRouter) SetDefaultHandler(handler http.Handler) {
	vr.defaultHandler = handler
}

// ServeHTTP routes requests to appropriate version handlers
func (vr *VersionRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract version
	version, err := vr.versionManager.ExtractVersion(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid API version: %v", err), http.StatusBadRequest)
		return
	}

	// Find compatible handler
	handler := vr.findCompatibleHandler(version)
	if handler == nil {
		handler = vr.defaultHandler
	}

	if handler == nil {
		http.Error(w, "No handler available for API version", http.StatusNotImplemented)
		return
	}

	// Set version headers
	vr.versionManager.SetVersionHeaders(w, version)

	// Set version in context
	ctx := context.WithValue(r.Context(), "api_version", version)
	r = r.WithContext(ctx)

	// Serve request
	handler.ServeHTTP(w, r)
}

// findCompatibleHandler finds a handler compatible with the requested version
func (vr *VersionRouter) findCompatibleHandler(requestedVersion APIVersion) http.Handler {
	// First try exact match
	if handler, exists := vr.handlers[requestedVersion.String()]; exists {
		return handler.Handler
	}

	// Find the best compatible version (highest compatible version)
	var bestHandler *VersionedHandler
	for _, handler := range vr.handlers {
		if requestedVersion.IsCompatibleWith(handler.Version) {
			if bestHandler == nil || handler.Version.Compare(bestHandler.Version) > 0 {
				bestHandler = handler
			}
		}
	}

	if bestHandler != nil {
		return bestHandler.Handler
	}

	return nil
}

// Helper functions

// parseVersion parses a version string like "v1.0.0" or "1.0.0"
func parseVersion(versionStr string) (APIVersion, error) {
	// Remove 'v' prefix if present
	versionStr = strings.TrimPrefix(versionStr, "v")

	parts := strings.Split(versionStr, ".")
	if len(parts) < 1 {
		return APIVersion{}, fmt.Errorf("invalid version format: %s", versionStr)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return APIVersion{}, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor := 0
	if len(parts) > 1 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return APIVersion{}, fmt.Errorf("invalid minor version: %s", parts[1])
		}
	}

	patch := 0
	if len(parts) > 2 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return APIVersion{}, fmt.Errorf("invalid patch version: %s", parts[2])
		}
	}

	return APIVersion{Major: major, Minor: minor, Patch: patch}, nil
}

// JSONRPCVersioning adds versioning support to JSON-RPC requests
type JSONRPCVersioning struct {
	versionManager *VersionManager
}

// NewJSONRPCVersioning creates a new JSON-RPC versioning handler
func NewJSONRPCVersioning(versionManager *VersionManager) *JSONRPCVersioning {
	return &JSONRPCVersioning{
		versionManager: versionManager,
	}
}

// WrapHandler wraps a JSON-RPC handler with versioning support
func (jv *JSONRPCVersioning) WrapHandler(next func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse) func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	return func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
		// Check if version is specified in request params
		if req.Params != nil {
			if params, ok := req.Params.(map[string]interface{}); ok {
				if versionStr, ok := params["apiVersion"].(string); ok {
					version, err := parseVersion(versionStr)
					if err != nil {
						return &protocol.JSONRPCResponse{
							JSONRPC: "2.0",
							ID:      req.ID,
							Error:   protocol.NewJSONRPCError(protocol.InvalidParams, fmt.Sprintf("Invalid API version: %v", err), nil),
						}
					}

					// Validate version
					if err := jv.versionManager.ValidateVersion(version); err != nil {
						return &protocol.JSONRPCResponse{
							JSONRPC: "2.0",
							ID:      req.ID,
							Error:   protocol.NewJSONRPCError(protocol.InvalidParams, err.Error(), nil),
						}
					}

					// Set version in context
					ctx = context.WithValue(ctx, "api_version", version)
				}
			}
		}

		return next(ctx, req)
	}
}
