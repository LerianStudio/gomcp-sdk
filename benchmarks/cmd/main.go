// Performance testing CLI for MCP SDK
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"benchmarks"
)

func main() {
	var (
		outputDir = flag.String("output", "./benchmark_results", "Output directory for benchmark results")
		baseline  = flag.Bool("baseline", false, "Save current results as new baseline")
		report    = flag.Bool("report", true, "Generate human-readable report")
		check     = flag.Bool("check", true, "Check for performance regressions")
		verbose   = flag.Bool("verbose", false, "Verbose output")
	)
	flag.Parse()

	fmt.Println("🚀 MCP SDK Performance Testing Suite")
	fmt.Println("=====================================")

	// Create performance runner
	runner := benchmarks.NewPerformanceRunner(*outputDir)

	// Run benchmarks
	fmt.Println("📊 Running comprehensive benchmarks...")
	summary, err := runner.RunBenchmarks()
	if err != nil {
		log.Fatalf("❌ Failed to run benchmarks: %v", err)
	}

	fmt.Printf("✅ Completed %d benchmarks\n", summary.TotalBenchmarks)

	// Save as baseline if requested
	if *baseline {
		if err := runner.SaveBaseline(summary); err != nil {
			log.Fatalf("❌ Failed to save baseline: %v", err)
		}
		fmt.Println("💾 Results saved as new performance baseline")
	}

	// Check for regressions
	var regressionReport *benchmarks.RegressionReport
	if *check {
		fmt.Println("🔍 Checking for performance regressions...")
		regressionReport, err = runner.CheckRegression(summary)
		if err != nil {
			log.Fatalf("❌ Failed to check regressions: %v", err)
		}

		if regressionReport.HasRegression {
			fmt.Printf("⚠️  Found %d performance regressions\n", len(regressionReport.Regressions))
			if *verbose {
				for _, reg := range regressionReport.Regressions {
					fmt.Printf("   - %s: %.2f%% slower (%s)\n",
						reg.BenchmarkName, reg.ChangePercent, reg.Severity)
				}
			}
		} else {
			fmt.Println("✅ No performance regressions detected")
		}

		if len(regressionReport.Improvements) > 0 {
			fmt.Printf("🎉 Found %d performance improvements\n", len(regressionReport.Improvements))
			if *verbose {
				for _, imp := range regressionReport.Improvements {
					fmt.Printf("   - %s: %.2f%% faster\n",
						imp.BenchmarkName, -imp.ChangePercent)
				}
			}
		}
	}

	// Generate report
	if *report {
		fmt.Println("📝 Generating performance report...")
		reportContent := runner.GenerateReport(summary, regressionReport)

		reportFile := filepath.Join(*outputDir, "performance_report.md")
		if err := os.WriteFile(reportFile, []byte(reportContent), 0600); err != nil {
			log.Fatalf("❌ Failed to write report: %v", err)
		}

		fmt.Printf("📄 Report written to: %s\n", reportFile)
	}

	// Summary output
	fmt.Println("\n📈 Performance Summary:")
	if summaryStats, ok := summary.Summary["average_ns_per_op"].(float64); ok {
		fmt.Printf("   Average Performance: %.2f ns/op\n", summaryStats)
	}
	if summaryStats, ok := summary.Summary["fastest_ns_per_op"].(float64); ok {
		fmt.Printf("   Fastest Benchmark: %.2f ns/op\n", summaryStats)
	}
	if summaryStats, ok := summary.Summary["slowest_ns_per_op"].(float64); ok {
		fmt.Printf("   Slowest Benchmark: %.2f ns/op\n", summaryStats)
	}

	// Exit with error code if regressions found
	if regressionReport != nil && regressionReport.HasRegression {
		fmt.Println("\n❌ Performance regressions detected!")
		os.Exit(1)
	}

	fmt.Println("\n✅ Performance testing completed successfully!")
}
