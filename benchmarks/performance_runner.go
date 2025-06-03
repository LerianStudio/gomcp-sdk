// Package benchmarks provides tools for running performance tests and detecting regressions
package benchmarks

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BenchmarkResult represents the result of a benchmark run
type BenchmarkResult struct {
	Name           string             `json:"name"`
	Iterations     int                `json:"iterations"`
	NsPerOp        float64            `json:"ns_per_op"`
	AllocsPerOp    int                `json:"allocs_per_op"`
	BytesPerOp     int                `json:"bytes_per_op"`
	MemAllocsPerOp int                `json:"mem_allocs_per_op"`
	MemBytesPerOp  int                `json:"mem_bytes_per_op"`
	CustomMetrics  map[string]float64 `json:"custom_metrics"`
	Timestamp      time.Time          `json:"timestamp"`
}

// BenchmarkSummary contains summary statistics for a benchmark run
type BenchmarkSummary struct {
	TotalBenchmarks int                    `json:"total_benchmarks"`
	Results         []BenchmarkResult      `json:"results"`
	Summary         map[string]interface{} `json:"summary"`
	Timestamp       time.Time              `json:"timestamp"`
	GitCommit       string                 `json:"git_commit,omitempty"`
	GitBranch       string                 `json:"git_branch,omitempty"`
}

// PerformanceRunner manages benchmark execution and regression detection
type PerformanceRunner struct {
	outputDir           string
	baselineFile        string
	regressionThreshold float64 // Percentage threshold for regression detection
}

// NewPerformanceRunner creates a new performance runner
func NewPerformanceRunner(outputDir string) *PerformanceRunner {
	return &PerformanceRunner{
		outputDir:           outputDir,
		baselineFile:        filepath.Join(outputDir, "baseline.json"),
		regressionThreshold: 10.0, // 10% performance degradation threshold
	}
}

// RunBenchmarks executes all benchmarks and returns results
func (pr *PerformanceRunner) RunBenchmarks() (*BenchmarkSummary, error) {
	// Ensure output directory exists
	if err := os.MkdirAll(pr.outputDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Run benchmarks with JSON output
	cmd := exec.Command("go", "test", "-bench=.", "-benchmem", "-benchtime=3s", "-count=3", "-json")
	cmd.Dir = "."

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run benchmarks: %w", err)
	}

	// Parse benchmark output
	results, err := pr.parseBenchmarkOutput(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse benchmark output: %w", err)
	}

	// Get git information
	gitCommit, _ := pr.getGitCommit()
	gitBranch, _ := pr.getGitBranch()

	summary := &BenchmarkSummary{
		TotalBenchmarks: len(results),
		Results:         results,
		Timestamp:       time.Now(),
		GitCommit:       gitCommit,
		GitBranch:       gitBranch,
		Summary:         pr.generateSummaryStats(results),
	}

	// Save results
	if err := pr.saveResults(summary); err != nil {
		return nil, fmt.Errorf("failed to save results: %w", err)
	}

	return summary, nil
}

// CheckRegression compares current results against baseline
func (pr *PerformanceRunner) CheckRegression(current *BenchmarkSummary) (*RegressionReport, error) {
	baseline, err := pr.loadBaseline()
	if err != nil {
		// No baseline exists, save current as baseline
		if err := pr.SaveBaseline(current); err != nil {
			return nil, fmt.Errorf("failed to save baseline: %w", err)
		}
		return &RegressionReport{
			HasRegression: false,
			Message:       "No baseline found. Current results saved as new baseline.",
		}, nil
	}

	return pr.compareResults(baseline, current), nil
}

// RegressionReport contains regression analysis results
type RegressionReport struct {
	HasRegression  bool                   `json:"has_regression"`
	Regressions    []RegressionDetail     `json:"regressions"`
	Improvements   []RegressionDetail     `json:"improvements"`
	Message        string                 `json:"message"`
	OverallSummary map[string]interface{} `json:"overall_summary"`
}

// RegressionDetail contains details about a specific regression
type RegressionDetail struct {
	BenchmarkName string  `json:"benchmark_name"`
	Metric        string  `json:"metric"`
	BaselineValue float64 `json:"baseline_value"`
	CurrentValue  float64 `json:"current_value"`
	ChangePercent float64 `json:"change_percent"`
	Severity      string  `json:"severity"` // "minor", "major", "critical"
}

// parseBenchmarkOutput parses the JSON output from go test
func (pr *PerformanceRunner) parseBenchmarkOutput(output []byte) ([]BenchmarkResult, error) {
	lines := strings.Split(string(output), "\n")
	var results []BenchmarkResult

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var testOutput struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
			Output  string `json:"Output"`
		}

		if err := json.Unmarshal([]byte(line), &testOutput); err != nil {
			continue // Skip non-JSON lines
		}

		if testOutput.Action == "output" && strings.Contains(testOutput.Output, "Benchmark") {
			if result := pr.parseBenchmarkLine(testOutput.Output); result != nil {
				results = append(results, *result)
			}
		}
	}

	return results, nil
}

// parseBenchmarkLine parses a single benchmark output line
func (pr *PerformanceRunner) parseBenchmarkLine(line string) *BenchmarkResult {
	// Expected format: BenchmarkName-N    iterations    ns/op    allocs/op    B/op
	parts := strings.Fields(line)
	if len(parts) < 4 || !strings.HasPrefix(parts[0], "Benchmark") {
		return nil
	}

	name := parts[0]
	iterations, _ := strconv.Atoi(parts[1])
	nsPerOp, _ := strconv.ParseFloat(parts[2], 64)

	result := &BenchmarkResult{
		Name:          name,
		Iterations:    iterations,
		NsPerOp:       nsPerOp,
		CustomMetrics: make(map[string]float64),
		Timestamp:     time.Now(),
	}

	// Parse additional metrics if available
	if len(parts) >= 6 {
		result.AllocsPerOp, _ = strconv.Atoi(parts[4])
		result.BytesPerOp, _ = strconv.Atoi(parts[5])
	}

	return result
}

// generateSummaryStats generates summary statistics
func (pr *PerformanceRunner) generateSummaryStats(results []BenchmarkResult) map[string]interface{} {
	if len(results) == 0 {
		return map[string]interface{}{}
	}

	var totalNsPerOp, totalAllocs, totalBytes float64
	fastestNs, slowestNs := results[0].NsPerOp, results[0].NsPerOp

	for _, result := range results {
		totalNsPerOp += result.NsPerOp
		totalAllocs += float64(result.AllocsPerOp)
		totalBytes += float64(result.BytesPerOp)

		if result.NsPerOp < fastestNs {
			fastestNs = result.NsPerOp
		}
		if result.NsPerOp > slowestNs {
			slowestNs = result.NsPerOp
		}
	}

	count := float64(len(results))
	return map[string]interface{}{
		"average_ns_per_op":     totalNsPerOp / count,
		"average_allocs_per_op": totalAllocs / count,
		"average_bytes_per_op":  totalBytes / count,
		"fastest_ns_per_op":     fastestNs,
		"slowest_ns_per_op":     slowestNs,
		"total_benchmarks":      len(results),
	}
}

// saveResults saves benchmark results to file
func (pr *PerformanceRunner) saveResults(summary *BenchmarkSummary) error {
	filename := fmt.Sprintf("benchmark_%s.json", summary.Timestamp.Format("20060102_150405"))
	filepath := filepath.Join(pr.outputDir, filename)

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0600)
}

// SaveBaseline saves current results as baseline
func (pr *PerformanceRunner) SaveBaseline(summary *BenchmarkSummary) error {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(pr.baselineFile, data, 0600)
}

// loadBaseline loads baseline results
func (pr *PerformanceRunner) loadBaseline() (*BenchmarkSummary, error) {
	data, err := os.ReadFile(pr.baselineFile)
	if err != nil {
		return nil, err
	}

	var summary BenchmarkSummary
	err = json.Unmarshal(data, &summary)
	return &summary, err
}

// compareResults compares current results with baseline
func (pr *PerformanceRunner) compareResults(baseline, current *BenchmarkSummary) *RegressionReport {
	report := &RegressionReport{
		Regressions:    []RegressionDetail{},
		Improvements:   []RegressionDetail{},
		OverallSummary: make(map[string]interface{}),
	}

	// Create lookup map for baseline results
	baselineMap := make(map[string]BenchmarkResult)
	for _, result := range baseline.Results {
		baselineMap[result.Name] = result
	}

	var totalRegressions, totalImprovements int

	for _, currentResult := range current.Results {
		baselineResult, exists := baselineMap[currentResult.Name]
		if !exists {
			continue // New benchmark, skip comparison
		}

		// Compare ns/op (primary metric)
		changePercent := ((currentResult.NsPerOp - baselineResult.NsPerOp) / baselineResult.NsPerOp) * 100

		detail := RegressionDetail{
			BenchmarkName: currentResult.Name,
			Metric:        "ns/op",
			BaselineValue: baselineResult.NsPerOp,
			CurrentValue:  currentResult.NsPerOp,
			ChangePercent: changePercent,
		}

		if changePercent > pr.regressionThreshold {
			// Performance regression
			detail.Severity = pr.calculateSeverity(changePercent)
			report.Regressions = append(report.Regressions, detail)
			totalRegressions++
		} else if changePercent < -pr.regressionThreshold {
			// Performance improvement
			detail.Severity = "improvement"
			report.Improvements = append(report.Improvements, detail)
			totalImprovements++
		}
	}

	report.HasRegression = len(report.Regressions) > 0

	if report.HasRegression {
		report.Message = fmt.Sprintf("Found %d performance regressions and %d improvements",
			totalRegressions, totalImprovements)
	} else if totalImprovements > 0 {
		report.Message = fmt.Sprintf("Found %d performance improvements, no regressions",
			totalImprovements)
	} else {
		report.Message = "No significant performance changes detected"
	}

	report.OverallSummary = map[string]interface{}{
		"total_regressions":  totalRegressions,
		"total_improvements": totalImprovements,
		"baseline_timestamp": baseline.Timestamp,
		"current_timestamp":  current.Timestamp,
	}

	return report
}

// calculateSeverity calculates regression severity based on change percentage
func (pr *PerformanceRunner) calculateSeverity(changePercent float64) string {
	if changePercent >= 50 {
		return "critical"
	} else if changePercent >= 25 {
		return "major"
	}
	return "minor"
}

// getGitCommit gets current git commit hash
func (pr *PerformanceRunner) getGitCommit() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getGitBranch gets current git branch
func (pr *PerformanceRunner) getGitBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// GenerateReport generates a human-readable performance report
func (pr *PerformanceRunner) GenerateReport(summary *BenchmarkSummary, regression *RegressionReport) string {
	var report strings.Builder

	report.WriteString("# MCP SDK Performance Report\n\n")
	report.WriteString(fmt.Sprintf("**Generated:** %s\n", summary.Timestamp.Format(time.RFC3339)))
	report.WriteString(fmt.Sprintf("**Git Commit:** %s\n", summary.GitCommit))
	report.WriteString(fmt.Sprintf("**Git Branch:** %s\n\n", summary.GitBranch))

	report.WriteString("## Summary\n\n")
	report.WriteString(fmt.Sprintf("- **Total Benchmarks:** %d\n", summary.TotalBenchmarks))

	if summaryStats, ok := summary.Summary["average_ns_per_op"].(float64); ok {
		report.WriteString(fmt.Sprintf("- **Average Performance:** %.2f ns/op\n", summaryStats))
	}

	if regression != nil {
		report.WriteString(fmt.Sprintf("- **Regression Status:** %s\n", regression.Message))
		report.WriteString(fmt.Sprintf("- **Regressions Found:** %d\n", len(regression.Regressions)))
		report.WriteString(fmt.Sprintf("- **Improvements Found:** %d\n\n", len(regression.Improvements)))

		if len(regression.Regressions) > 0 {
			report.WriteString("## Performance Regressions\n\n")
			for _, reg := range regression.Regressions {
				report.WriteString(fmt.Sprintf("- **%s** (%s): %.2f%% slower (%.2f → %.2f ns/op)\n",
					reg.BenchmarkName, reg.Severity, reg.ChangePercent,
					reg.BaselineValue, reg.CurrentValue))
			}
			report.WriteString("\n")
		}

		if len(regression.Improvements) > 0 {
			report.WriteString("## Performance Improvements\n\n")
			for _, imp := range regression.Improvements {
				report.WriteString(fmt.Sprintf("- **%s**: %.2f%% faster (%.2f → %.2f ns/op)\n",
					imp.BenchmarkName, -imp.ChangePercent,
					imp.BaselineValue, imp.CurrentValue))
			}
			report.WriteString("\n")
		}
	}

	report.WriteString("## Detailed Results\n\n")
	report.WriteString("| Benchmark | Iterations | ns/op | Allocs/op | Bytes/op |\n")
	report.WriteString("|-----------|------------|-------|-----------|----------|\n")

	for _, result := range summary.Results {
		report.WriteString(fmt.Sprintf("| %s | %d | %.2f | %d | %d |\n",
			result.Name, result.Iterations, result.NsPerOp,
			result.AllocsPerOp, result.BytesPerOp))
	}

	return report.String()
}
