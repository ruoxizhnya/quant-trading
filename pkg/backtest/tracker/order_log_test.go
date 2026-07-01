package tracker

import (
	"testing"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestOrderLog_RecordAndGetOrders(t *testing.T) {
	log := &OrderLog{}

	orders := log.GetOrders()
	assert.Empty(t, orders)

	o1 := domain.Order{
		Symbol:    "600000.SH",
		Direction: domain.DirectionLong,
		Quantity:  100,
		FillPrice: 10.0,
		Timestamp: time.Now(),
	}
	log.Record(o1)

	o2 := domain.Order{
		Symbol:    "600001.SH",
		Direction: domain.DirectionClose,
		Quantity:  200,
		FillPrice: 20.0,
		Timestamp: time.Now(),
	}
	log.Record(o2)

	all := log.GetOrders()
	assert.Len(t, all, 2)
	assert.Equal(t, "600000.SH", all[0].Symbol)
	assert.Equal(t, "600001.SH", all[1].Symbol)
}

func TestOrderLog_Empty(t *testing.T) {
	log := &OrderLog{}
	orders := log.GetOrders()
	assert.Nil(t, orders)
}
