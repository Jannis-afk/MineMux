package main

import "testing"

func TestWorldChunkSectionUsesPaddedBlockStateStorage(t *testing.T) {
	palette := make([]any, 17)
	for i := range palette {
		palette[i] = nbtCompound{"Name": "test:block_" + string(rune('a'+i))}
	}
	indexes := map[int]int{
		0:   1,
		11:  11,
		12:  12,
		255: 15,
		256: 16,
	}
	section, ok := parseWorldChunkSection(nbtCompound{
		"Y": int8(0),
		"block_states": nbtCompound{
			"palette": palette,
			"data":    packPaddedBlockStatesForTest(5, indexes),
		},
	})
	if !ok {
		t.Fatal("section did not parse")
	}
	if section.Compact {
		t.Fatal("modern padded block-state storage was detected as compact")
	}
	tests := []struct {
		x, y, z int
		want    string
	}{
		{x: 0, y: 0, z: 0, want: "test:block_b"},
		{x: 11, y: 0, z: 0, want: "test:block_l"},
		{x: 12, y: 0, z: 0, want: "test:block_m"},
		{x: 15, y: 0, z: 15, want: "test:block_p"},
		{x: 0, y: 1, z: 0, want: "test:block_q"},
	}
	for _, test := range tests {
		if got := section.blockAt(test.x, test.y, test.z); got != test.want {
			t.Fatalf("blockAt(%d,%d,%d) = %q, want %q", test.x, test.y, test.z, got, test.want)
		}
	}
}

func packPaddedBlockStatesForTest(bits int, indexes map[int]int) []int64 {
	valuesPerLong := 64 / bits
	data := make([]int64, (4096+valuesPerLong-1)/valuesPerLong)
	mask := uint64((1 << bits) - 1)
	for blockIndex, paletteIndex := range indexes {
		longIndex := blockIndex / valuesPerLong
		offset := (blockIndex - longIndex*valuesPerLong) * bits
		data[longIndex] |= int64((uint64(paletteIndex) & mask) << offset)
	}
	return data
}
