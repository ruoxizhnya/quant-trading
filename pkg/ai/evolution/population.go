package evolution

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/ai/gene_pool"
)

// Population manages a population of strategy genes.
type Population struct {
	individuals []*gene_pool.StrategyGene
	generation  int
	maxSize     int
}

// NewPopulation creates a new population.
func NewPopulation(maxSize int) *Population {
	return &Population{
		individuals: make([]*gene_pool.StrategyGene, 0, maxSize),
		generation:  0,
		maxSize:     maxSize,
	}
}

// Add adds an individual to the population.
func (p *Population) Add(gene *gene_pool.StrategyGene) error {
	if len(p.individuals) >= p.maxSize {
		return fmt.Errorf("population at maximum size: %d", p.maxSize)
	}
	p.individuals = append(p.individuals, gene)
	return nil
}

// Remove removes an individual by ID.
func (p *Population) Remove(id string) bool {
	for i, ind := range p.individuals {
		if ind.ID == id {
			p.individuals = append(p.individuals[:i], p.individuals[i+1:]...)
			return true
		}
	}
	return false
}

// Get returns an individual by ID.
func (p *Population) Get(id string) *gene_pool.StrategyGene {
	for _, ind := range p.individuals {
		if ind.ID == id {
			return ind
		}
	}
	return nil
}

// GetAll returns all individuals.
func (p *Population) GetAll() []*gene_pool.StrategyGene {
	result := make([]*gene_pool.StrategyGene, len(p.individuals))
	copy(result, p.individuals)
	return result
}

// GetTop returns the top N individuals by fitness.
func (p *Population) GetTop(n int) []*gene_pool.StrategyGene {
	p.sortByFitness()
	if n > len(p.individuals) {
		n = len(p.individuals)
	}
	result := make([]*gene_pool.StrategyGene, n)
	copy(result, p.individuals[:n])
	return result
}

// Size returns the current population size.
func (p *Population) Size() int {
	return len(p.individuals)
}

// GetGeneration returns the current generation number.
func (p *Population) GetGeneration() int {
	return p.generation
}

// IncrementGeneration increments the generation counter.
func (p *Population) IncrementGeneration() {
	p.generation++
}

// GetAverageFitness returns the average fitness of the population.
func (p *Population) GetAverageFitness() float64 {
	if len(p.individuals) == 0 {
		return 0
	}
	var sum float64
	for _, ind := range p.individuals {
		sum += ind.Fitness
	}
	return sum / float64(len(p.individuals))
}

// GetBestFitness returns the best fitness in the population.
func (p *Population) GetBestFitness() float64 {
	if len(p.individuals) == 0 {
		return 0
	}
	best := p.individuals[0].Fitness
	for _, ind := range p.individuals[1:] {
		if ind.Fitness > best {
			best = ind.Fitness
		}
	}
	return best
}

// GetDiversity returns a measure of population diversity.
func (p *Population) GetDiversity() float64 {
	if len(p.individuals) < 2 {
		return 0
	}

	// Calculate average pairwise fitness difference
	var totalDiff float64
	count := 0
	for i := 0; i < len(p.individuals); i++ {
		for j := i + 1; j < len(p.individuals); j++ {
			diff := p.individuals[i].Fitness - p.individuals[j].Fitness
			if diff < 0 {
				diff = -diff
			}
			totalDiff += diff
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return totalDiff / float64(count)
}

// Prune removes the worst individuals to maintain max size.
func (p *Population) Prune() int {
	if len(p.individuals) <= p.maxSize {
		return 0
	}

	p.sortByFitness()
	removed := len(p.individuals) - p.maxSize
	p.individuals = p.individuals[:p.maxSize]
	return removed
}

// SaveToPool persists the population to the gene pool.
func (p *Population) SaveToPool(ctx context.Context, pool *gene_pool.StrategyPool) error {
	for _, ind := range p.individuals {
		ind.Generation = p.generation
		ind.UpdatedAt = time.Now()
		if err := pool.Save(ctx, ind); err != nil {
			return fmt.Errorf("save individual %s: %w", ind.ID, err)
		}
	}
	return nil
}

// LoadFromPool loads individuals from the gene pool.
func (p *Population) LoadFromPool(ctx context.Context, pool *gene_pool.StrategyPool, generation int) error {
	genes, err := pool.ListByGeneration(ctx, generation)
	if err != nil {
		return fmt.Errorf("load generation %d: %w", generation, err)
	}

	for _, gene := range genes {
		if err := p.Add(gene); err != nil {
			// Population might be full, stop loading
			break
		}
	}

	p.generation = generation
	return nil
}

// sortByFitness sorts individuals by fitness descending.
func (p *Population) sortByFitness() {
	sort.Slice(p.individuals, func(i, j int) bool {
		return p.individuals[i].Fitness > p.individuals[j].Fitness
	})
}

// PopulationStats holds statistics about the population.
type PopulationStats struct {
	Size         int     `json:"size"`
	Generation   int     `json:"generation"`
	BestFitness  float64 `json:"best_fitness"`
	AvgFitness   float64 `json:"avg_fitness"`
	WorstFitness float64 `json:"worst_fitness"`
	Diversity    float64 `json:"diversity"`
	MaxSize      int     `json:"max_size"`
}

// GetStats returns population statistics.
func (p *Population) GetStats() *PopulationStats {
	if len(p.individuals) == 0 {
		return &PopulationStats{
			Size:       0,
			Generation: p.generation,
			MaxSize:    p.maxSize,
		}
	}

	best := p.individuals[0].Fitness
	worst := p.individuals[0].Fitness
	sum := 0.0

	for _, ind := range p.individuals {
		if ind.Fitness > best {
			best = ind.Fitness
		}
		if ind.Fitness < worst {
			worst = ind.Fitness
		}
		sum += ind.Fitness
	}

	return &PopulationStats{
		Size:         len(p.individuals),
		Generation:   p.generation,
		BestFitness:  best,
		AvgFitness:   sum / float64(len(p.individuals)),
		WorstFitness: worst,
		Diversity:    p.GetDiversity(),
		MaxSize:      p.maxSize,
	}
}
