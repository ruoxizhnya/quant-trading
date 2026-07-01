package batch

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
)

func createTempCSV(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.csv")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

func TestParseBatchCSV_MissingRequiredColumn(t *testing.T) {
	csvContent := `strategy,stock_pool,start_date
momentum,AAPL,2024-01-01
`
	path := createTempCSV(t, csvContent)

	_, err := ParseBatchCSV(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required column")
}

func TestParseBatchCSV_FileNotFound(t *testing.T) {
	_, err := ParseBatchCSV("/nonexistent/path.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "open csv")
}

func TestParseBatchCSV_EmptyFile(t *testing.T) {
	csvContent := `strategy,stock_pool,start_date,end_date
`
	path := createTempCSV(t, csvContent)

	_, err := ParseBatchCSV(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "header + at least 1 data row")
}

func TestParseBatchCSV_InvalidCapital(t *testing.T) {
	csvContent := `strategy,stock_pool,start_date,end_date,capital
momentum,AAPL,2024-01-01,2024-12-31,invalid
`
	path := createTempCSV(t, csvContent)

	tasks, err := ParseBatchCSV(path)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, 1000000.0, tasks[0].Capital) // default fallback
}

func TestParseBatchCSV_WhitespaceTrimmed(t *testing.T) {
	csvContent := `strategy , stock_pool ,start_date ,end_date
 momentum , AAPL , 2024-01-01 , 2024-12-31
`
	path := createTempCSV(t, csvContent)

	tasks, err := ParseBatchCSV(path)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "momentum", tasks[0].Strategy)
	assert.Equal(t, "AAPL", tasks[0].StockPool[0])
}

func TestParseBatchCSV_EmptyRowsSkipped(t *testing.T) {
	csvContent := `strategy,stock_pool,start_date,end_date
momentum,AAPL,2024-01-01,2024-12-31
,,,
value,MSFT,2024-01-01,2024-06-30
`
	path := createTempCSV(t, csvContent)

	tasks, err := ParseBatchCSV(path)
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
}

func TestParseJinCeCSV_MixedColumns(t *testing.T) {
	// Mix of Chinese and English column names
	csvContent := `策略名称,stock_pool,开始日期,end_date
quality,AAPL,2024-01-01,2024-12-31
`
	path := createTempCSV(t, csvContent)

	tasks, err := ParseJinCeCSV(path)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "quality", tasks[0].Strategy)
}

func TestParseJinCeCSV_MissingRequiredColumn(t *testing.T) {
	csvContent := `策略名称,股票池,开始日期
动量策略,AAPL,2024-01-01
`
	path := createTempCSV(t, csvContent)

	_, err := ParseJinCeCSV(path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "金策格式")
}

func TestGenerateTaskMatrix_Empty(t *testing.T) {
	tasks := GenerateTaskMatrix([]string{}, [][]string{}, [][2]string{})
	assert.Empty(t, tasks)
}

func TestExportBatchReportCSV_WithResults(t *testing.T) {
	report := &BatchReport{
		BatchID: "test_batch",
		Results: []*BatchResult{
			{
				TaskID:   "task_1",
				Strategy: "momentum",
				Status:   "completed",
				Result: &domain.BacktestResult{
					SharpeRatio:  1.5,
					AnnualReturn: 0.25,
					MaxDrawdown:  -0.1,
					WinRate:      0.6,
					TotalTrades:  20,
					CalmarRatio:  2.5,
					SortinoRatio: 1.8,
				},
				Score: &BatchScore{
					Grade:          "A",
					OverfitScore:   0.2,
					StabilityScore: 0.8,
					CompositeScore: 0.85,
					Rank:           1,
				},
			},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "report.csv")

	err := ExportBatchReportCSV(report, path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "A")
	assert.Contains(t, content, "1.5000")
	assert.Contains(t, content, "0.8500")
}
