package dagtraversalmanager_test

import (
	"github.com/kaspanet/kaspad/domain/consensus"
	"github.com/kaspanet/kaspad/domain/consensus/model/externalapi"
	"github.com/kaspanet/kaspad/domain/consensus/utils/hashset"
	"github.com/kaspanet/kaspad/domain/consensus/utils/testutils"
	"github.com/kaspanet/kaspad/domain/dagconfig"
	"github.com/pkg/errors"
	"reflect"
	"testing"
)

func TestBlueBlockWindow(t *testing.T) {
	testutils.ForAllNets(t, true, func(t *testing.T, params *dagconfig.Params) {
		factory := consensus.NewFactory()
		tc, tearDown, err := factory.NewTestConsensus(params, "TestBlueBlockWindow")
		if err != nil {
			t.Fatalf("NewTestConsensus: %s", err)
		}
		defer tearDown()

		windowSize := uint64(10)
		blockByIDMap := make(map[string]*externalapi.DomainHash)
		idByBlockMap := make(map[externalapi.DomainHash]string)
		blockByIDMap["A"] = params.GenesisHash
		idByBlockMap[*params.GenesisHash] = "A"

		blocksData := []*struct {
			parents                          []string
			id                               string //id is a virtual entity that is used only for tests so we can define relations between blocks without knowing their hash
			expectedWindowWithGenesisPadding []string
		}{
			{
				parents:                          []string{"A"},
				id:                               "B",
				expectedWindowWithGenesisPadding: []string{"A", "A", "A", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"B"},
				id:                               "C",
				expectedWindowWithGenesisPadding: []string{"B", "A", "A", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"B"},
				id:                               "D",
				expectedWindowWithGenesisPadding: []string{"B", "A", "A", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"C", "D"},
				id:                               "E",
				expectedWindowWithGenesisPadding: []string{"D", "C", "B", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"C", "D"},
				id:                               "F",
				expectedWindowWithGenesisPadding: []string{"D", "C", "B", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"A"},
				id:                               "G",
				expectedWindowWithGenesisPadding: []string{"A", "A", "A", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"G"},
				id:                               "H",
				expectedWindowWithGenesisPadding: []string{"G", "A", "A", "A", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"H", "F"},
				id:                               "I",
				expectedWindowWithGenesisPadding: []string{"F", "D", "C", "B", "A", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"I"},
				id:                               "J",
				expectedWindowWithGenesisPadding: []string{"I", "F", "D", "C", "B", "A", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"J"},
				id:                               "K",
				expectedWindowWithGenesisPadding: []string{"J", "I", "F", "D", "C", "B", "A", "A", "A", "A"},
			},
			{
				parents:                          []string{"K"},
				id:                               "L",
				expectedWindowWithGenesisPadding: []string{"K", "J", "I", "F", "D", "C", "B", "A", "A", "A"},
			},
			{
				parents:                          []string{"L"},
				id:                               "M",
				expectedWindowWithGenesisPadding: []string{"L", "K", "J", "I", "F", "D", "C", "B", "A", "A"},
			},
			{
				parents:                          []string{"M"},
				id:                               "N",
				expectedWindowWithGenesisPadding: []string{"M", "L", "K", "J", "I", "F", "D", "C", "B", "A"},
			},
			{
				parents:                          []string{"N"},
				id:                               "O",
				expectedWindowWithGenesisPadding: []string{"N", "M", "L", "K", "J", "I", "F", "D", "C", "B"},
			},
		}

		for _, blockData := range blocksData {
			parents := hashset.New()
			for _, parentID := range blockData.parents {
				parent := blockByIDMap[parentID]
				parents.Add(parent)
			}

			block, err := tc.AddBlock(parents.ToSlice(), nil, nil)
			if err != nil {
				t.Fatalf("AddBlock: %s", err)
			}

			blockByIDMap[blockData.id] = block
			idByBlockMap[*block] = blockData.id

			window, err := tc.DAGTraversalManager().BlueWindow(block, windowSize)
			if err != nil {
				t.Fatalf("BlueWindow: %s", err)
			}
			if err := checkWindowIDs(window, blockData.expectedWindowWithGenesisPadding, idByBlockMap); err != nil {
				t.Errorf("Unexpected values for window for block %s: %s", blockData.id, err)
			}
		}
	})
}

func checkWindowIDs(window []*externalapi.DomainHash, expectedIDs []string, idByBlockMap map[externalapi.DomainHash]string) error {
	ids := make([]string, len(window))
	for i, node := range window {
		ids[i] = idByBlockMap[*node]
	}
	if !reflect.DeepEqual(ids, expectedIDs) {
		return errors.Errorf("window expected to have blocks %s but got %s", expectedIDs, ids)
	}
	return nil
}