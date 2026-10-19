/*
===========================================================================

chainhop_test.go - the Chain Lightning hop reaches the bootstrap only when on

===========================================================================
*/
package enterworld

import (
	"encoding/json"
	"strings"
	"testing"
)

/*
================
TestChainLightningHopIsSentOnlyWhenOn

Native (nil) sends no chainLightningHop, so the blob is the one an older
build sent; the owner's setting sends the period and the family's ids in
the shape the client's chain-cadence.ts reads.
================
*/
func TestChainLightningHopIsSentOnlyWhenOn(t *testing.T) {
	for _, c := range []struct {
		hop  *ChainLightningHop
		want string
	}{
		{nil, ""},
		{&ChainLightningHop{HopMs: 500, Skills: []uint32{8982, 8983}}, `"chainLightningHop":{"hopMs":500,"skills":[8982,8983]}`},
	} {
		blob, err := json.Marshal(&BootstrapResult{
			NativeResult: nativeResultSuccess, Character: &Character{Name: "keeper", AccountID: "a"},
			LocalPlayerEntry: &LocalPlayerEntry{RaceKey: RaceKeyEurope},
			ChatMessages:     []string{}, SystemMessages: []string{}, Packets: []Packet{},
			ChainLightningHop: c.hop,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if c.want == "" && strings.Contains(string(blob), "chainLightningHop") {
			t.Fatal("a native shard named chainLightningHop in the blob")
		}
		if c.want != "" && !strings.Contains(string(blob), c.want) {
			t.Fatalf("blob lacks %s: %s", c.want, blob)
		}
	}
}
