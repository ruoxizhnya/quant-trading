-- Migration 012: Add gene pool tables for AI factor and strategy evolution
-- Date: 2026-05-05

-- Factor genes table
CREATE TABLE IF NOT EXISTS factor_genes (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    category VARCHAR(30) NOT NULL,
    formula TEXT NOT NULL,
    description TEXT,
    rationale TEXT,
    ic DOUBLE PRECISION DEFAULT 0,
    ir DOUBLE PRECISION DEFAULT 0,
    turnover DOUBLE PRECISION DEFAULT 0,
    sharpe DOUBLE PRECISION DEFAULT 0,
    fitness DOUBLE PRECISION DEFAULT 0,
    generation INTEGER DEFAULT 0,
    parent_ids JSONB DEFAULT '[]',
    status VARCHAR(20) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Strategy genes table
CREATE TABLE IF NOT EXISTS strategy_genes (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    strategy_type VARCHAR(30) NOT NULL,
    code TEXT,
    params JSONB DEFAULT '{}',
    factor_ids JSONB DEFAULT '[]',
    parent_ids JSONB DEFAULT '[]',
    total_return DOUBLE PRECISION DEFAULT 0,
    sharpe DOUBLE PRECISION DEFAULT 0,
    max_drawdown DOUBLE PRECISION DEFAULT 0,
    win_rate DOUBLE PRECISION DEFAULT 0,
    fitness DOUBLE PRECISION DEFAULT 0,
    generation INTEGER DEFAULT 0,
    status VARCHAR(20) DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for factor_genes
CREATE INDEX IF NOT EXISTS idx_factor_genes_category ON factor_genes(category);
CREATE INDEX IF NOT EXISTS idx_factor_genes_status ON factor_genes(status);
CREATE INDEX IF NOT EXISTS idx_factor_genes_fitness ON factor_genes(fitness DESC);
CREATE INDEX IF NOT EXISTS idx_factor_genes_ic ON factor_genes(ic DESC);
CREATE INDEX IF NOT EXISTS idx_factor_genes_generation ON factor_genes(generation);
CREATE INDEX IF NOT EXISTS idx_factor_genes_created_at ON factor_genes(created_at);

-- Indexes for strategy_genes
CREATE INDEX IF NOT EXISTS idx_strategy_genes_type ON strategy_genes(strategy_type);
CREATE INDEX IF NOT EXISTS idx_strategy_genes_status ON strategy_genes(status);
CREATE INDEX IF NOT EXISTS idx_strategy_genes_fitness ON strategy_genes(fitness DESC);
CREATE INDEX IF NOT EXISTS idx_strategy_genes_generation ON strategy_genes(generation);
CREATE INDEX IF NOT EXISTS idx_strategy_genes_created_at ON strategy_genes(created_at);

-- Composite indexes for common queries
CREATE INDEX IF NOT EXISTS idx_factor_genes_cat_status ON factor_genes(category, status);
CREATE INDEX IF NOT EXISTS idx_strategy_genes_type_status ON strategy_genes(strategy_type, status);
