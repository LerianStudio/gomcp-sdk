# MCP SDK Benchmarking Makefile

.PHONY: all bench bench-quick bench-full bench-regression bench-baseline bench-compare clean help

# Default target
all: bench

# Quick benchmark run (shorter duration)
bench-quick:
	@echo "🚀 Running quick benchmarks..."
	go test -bench=. -benchtime=1s -count=1

# Full benchmark run (longer duration for accuracy)
bench-full:
	@echo "🚀 Running comprehensive benchmarks..."
	go test -bench=. -benchmem -benchtime=5s -count=5

# Run benchmarks with regression detection
bench-regression:
	@echo "🔍 Running benchmarks with regression detection..."
	cd cmd && go run main.go -check=true -verbose=true

# Save current results as baseline
bench-baseline:
	@echo "💾 Saving benchmark baseline..."
	cd cmd && go run main.go -baseline=true -check=false

# Compare against baseline without updating
bench-compare:
	@echo "📊 Comparing against baseline..."
	cd cmd && go run main.go -baseline=false -check=true -verbose=true

# Generate detailed performance report
bench-report:
	@echo "📝 Generating performance report..."
	cd cmd && go run main.go -report=true -verbose=true

# Run memory profiling
bench-memory:
	@echo "🧠 Running memory profiling..."
	go test -bench=BenchmarkMemoryPatterns -memprofile=mem.prof -benchmem

# Run CPU profiling  
bench-cpu:
	@echo "⚡ Running CPU profiling..."
	go test -bench=BenchmarkServerOperations -cpuprofile=cpu.prof

# Run specific benchmark category
bench-protocol:
	go test -bench=BenchmarkProtocol -v

bench-transport:
	go test -bench=BenchmarkTransport -v

bench-server:
	go test -bench=BenchmarkServer -v

bench-scalability:
	go test -bench=BenchmarkScalability -v

# Profile analysis (requires pprof)
profile-cpu:
	@echo "📈 Analyzing CPU profile..."
	go tool pprof cpu.prof

profile-memory:
	@echo "🧠 Analyzing memory profile..."
	go tool pprof mem.prof

# Continuous benchmarking (for CI/CD)
bench-ci:
	@echo "🤖 Running CI benchmarks..."
	go test -bench=. -benchmem -benchtime=3s -count=3 -timeout=30m

# Clean benchmark artifacts
clean:
	@echo "🧹 Cleaning benchmark artifacts..."
	rm -rf benchmark_results/
	rm -f *.prof
	rm -f *.test

# Performance regression check for CI
check-regression:
	@echo "🔍 Checking for performance regressions (CI mode)..."
	cd cmd && go run main.go -check=true -report=true -verbose=false
	@if [ $$? -ne 0 ]; then \
		echo "❌ Performance regressions detected! Check the report."; \
		exit 1; \
	else \
		echo "✅ No performance regressions detected."; \
	fi

# Help target
help:
	@echo "MCP SDK Benchmarking Targets:"
	@echo ""
	@echo "Basic Benchmarks:"
	@echo "  bench-quick        - Quick benchmark run (1s, 1 iteration)"
	@echo "  bench-full         - Full benchmark run (5s, 5 iterations)"
	@echo "  bench              - Alias for bench-quick"
	@echo ""
	@echo "Regression Testing:"
	@echo "  bench-regression   - Run with regression detection"
	@echo "  bench-baseline     - Save current results as baseline"
	@echo "  bench-compare      - Compare against baseline"
	@echo "  check-regression   - CI-friendly regression check"
	@echo ""
	@echo "Profiling:"
	@echo "  bench-memory       - Memory profiling"
	@echo "  bench-cpu          - CPU profiling"
	@echo "  profile-cpu        - Analyze CPU profile"
	@echo "  profile-memory     - Analyze memory profile"
	@echo ""
	@echo "Category-specific:"
	@echo "  bench-protocol     - Protocol layer benchmarks"
	@echo "  bench-transport    - Transport layer benchmarks"
	@echo "  bench-server       - Server layer benchmarks"
	@echo "  bench-scalability  - Scalability benchmarks"
	@echo ""
	@echo "Utilities:"
	@echo "  bench-report       - Generate detailed report"
	@echo "  bench-ci           - CI/CD benchmarks"
	@echo "  clean              - Clean artifacts"
	@echo "  help               - Show this help"

# Set benchmark as default
bench: bench-quick