package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stevenwilkin/carry/deribit"

	_ "github.com/joho/godotenv/autoload"
)

const (
	msPerYear = 1000 * 60 * 60 * 24 * 365
	perpetual = "BTC-PERPETUAL"
)

type contract struct {
	Name      string  `json:"name"`
	Timestamp int64   `json:"timestamp"`
	Price     float64 `json:"price"`
	Index     float64 `json:"index"`
}

// marketData reads a single message from metricsd containing the state of
// every Deribit contract
func marketData() (map[string]contract, error) {
	u := url.URL{
		Scheme: "ws",
		Host:   fmt.Sprintf("%s:%s", os.Getenv("METRICSD_HOST"), os.Getenv("METRICSD_PORT")),
		Path:   "/ws"}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	_, message, err := c.ReadMessage()
	if err != nil {
		return nil, err
	}

	var contracts []contract
	if err := json.Unmarshal(message, &contracts); err != nil {
		return nil, err
	}

	result := make(map[string]contract, len(contracts))
	for _, c := range contracts {
		result[c.Name] = c
	}

	return result, nil
}

func fail(err error) {
	fmt.Println(err)
	os.Exit(1)
}

func main() {
	positions, err := deribit.NewDeribitFromEnv().GetPositions()
	if err != nil {
		fail(err)
	}

	contracts, err := marketData()
	if err != nil {
		fail(err)
	}

	now := time.Now().UnixMilli()

	var totalUSD, totalBTC, totalYield, totalAnnualised float64

	fmt.Printf("  %-13s %8s %10s %9s %10s %5s %9s %11s\n",
		"Contract", "Size", "BTC", "Premium", "Yield $", "Days", "Annual %", "Annual $")

	for _, p := range positions {
		if p.InstrumentName == perpetual {
			// no delivery so the premium isn't locked in
			fmt.Printf("  %-13s %8.0f   (excluded: no delivery date)\n",
				p.InstrumentName, math.Abs(p.Size))
			continue
		}

		c, ok := contracts[p.InstrumentName]
		if !ok {
			fail(fmt.Errorf("no market data for %s", p.InstrumentName))
		}

		// Deribit inverse futures are sized in USD, shorts are negative
		size := -p.Size
		btc := size / c.Index
		premium := c.Price - c.Index
		yield := btc * premium

		// annualised over each contract's own time to expiry, as in metrics
		years := float64(c.Timestamp-now) / msPerYear
		annualisedYield := (premium / c.Index) / years
		annualised := yield / years

		fmt.Printf("  %-13s %8.0f %10.4f %9.2f %10.2f %5.0f %8.2f%% %11.2f\n",
			c.Name, size, btc, premium, yield, years*365, annualisedYield*100, annualised)

		totalUSD += size
		totalBTC += btc
		totalYield += yield
		totalAnnualised += annualised
	}

	if totalUSD == 0 {
		fmt.Println("\n  No dated futures positions")
		return
	}

	// notional-weighted average of each contract's annualised yield
	annualisedYield := totalAnnualised / totalUSD

	fmt.Printf("  %-13s %8.0f %10.4f %9s %10.2f %5s %8.2f%% %11.2f\n\n",
		"Total", totalUSD, totalBTC, "", totalYield, "", annualisedYield*100, totalAnnualised)

	fmt.Printf("  Monthly: $%.2f\n", totalAnnualised/12)
}
