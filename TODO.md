# TODO List

This document tracks all TODO items found in the codebase. Items are organized by file and priority.

## High Priority

### Sampling Implementation
- **File:** `sampling/handler.go:34`
- **Task:** Implement actual sampling logic
- **Context:** Currently returns mock response. Need to integrate with LLM service for actual sampling functionality.

## Medium Priority

### Test Improvements

#### Resource Verification
- **File:** `testutil/scenarios.go:336`
- **Task:** Add resource list parsing and verification
- **Context:** Test scenario for resource listing needs proper response parsing and verification logic.

#### Content Verification
- **File:** `testutil/scenarios.go:353`
- **Task:** Add content verification
- **Context:** Test scenario for resource content needs verification implementation.

### Discovery Implementation
- **File:** `discovery/watcher.go:203`
- **Note:** Need to implement actual tool handler loading
- **Context:** In a real implementation, this would involve loading shared libraries or connecting to subprocesses.

## Low Priority

### Test Enhancements

#### SSE Event Parsing
- **File:** `transport/sse_test.go:413`
- **Note:** Need to properly parse SSE events in real implementation
- **Context:** Current test logs a placeholder message instead of parsing actual SSE events.

#### HTTPS Certificate Testing
- **File:** `transport/http_test.go:419`
- **Note:** Basic test implementation - production should use proper certificates
- **Context:** Current HTTPS transport test uses self-signed certificates.

## Documentation References

### Launch Checklist
- **File:** `docs/governance/LAUNCH_CHECKLIST.md:164`
- **Reference:** Mentions checking for TODO comments in critical items before launch

### Example Documentation
- **File:** `examples/file-browser/README.md:77,136`
- **Reference:** Uses "TODO" as an example search query in documentation

## Summary

Total TODO items found: 6 code items + 2 documentation references

Priority breakdown:
- High Priority: 1 (sampling implementation)
- Medium Priority: 3 (test improvements, discovery implementation)
- Low Priority: 2 (test enhancements)

Most TODOs are related to test improvements and implementation placeholders that need to be replaced with actual functionality.