package tempofb

import (
	"bytes"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/grafana/tempo/tempodb/encoding/common"
)

// SearchEntryMutable is a mutable form of the flatbuffer-compiled SearchEntry struct to make building and transporting easier.
type SearchEntryMutable struct {
	TraceID           common.ID
	Tags              SearchDataMap
	StartTimeUnixNano uint64
	EndTimeUnixNano   uint64
}

// AddTag adds the unique tag name and value to the search data. No effect if the pair is already present.
func (s *SearchEntryMutable) AddTag(k string, v string) {
	if s.Tags == nil {
		s.Tags = NewSearchDataMap()
	}
	s.Tags.Add(k, v)
}

// SetStartTimeUnixNano records the earliest of all timestamps passed to this function.
func (s *SearchEntryMutable) SetStartTimeUnixNano(t uint64) {
	if t > 0 && (s.StartTimeUnixNano == 0 || s.StartTimeUnixNano > t) {
		s.StartTimeUnixNano = t
	}
}

// SetEndTimeUnixNano records the latest of all timestamps passed to this function.
func (s *SearchEntryMutable) SetEndTimeUnixNano(t uint64) {
	if t > 0 && t > s.EndTimeUnixNano {
		s.EndTimeUnixNano = t
	}
}

func (s *SearchEntryMutable) ToBytes() []byte {
	b := flatbuffers.NewBuilder(2048)
	offset := s.WriteToBuilder(b)
	b.Finish(offset)
	return b.FinishedBytes()
}

func (s *SearchEntryMutable) WriteToBuilder(b *flatbuffers.Builder) flatbuffers.UOffsetT {
	if s.Tags == nil {
		s.Tags = NewSearchDataMap()
	}

	idOffset := b.CreateByteString(s.TraceID)

	tagOffset := s.Tags.WriteToBuilder(b)

	SearchEntryStart(b)
	SearchEntryAddId(b, idOffset)
	SearchEntryAddStartTimeUnixNano(b, s.StartTimeUnixNano)
	SearchEntryAddEndTimeUnixNano(b, s.EndTimeUnixNano)
	SearchEntryAddTags(b, tagOffset)
	return SearchEntryEnd(b)
}

type SearchPageBuilder struct {
	builder     *flatbuffers.Builder
	allTags     SearchDataMap
	pageEntries []flatbuffers.UOffsetT
}

func NewSearchPageBuilder() *SearchPageBuilder {
	return &SearchPageBuilder{
		builder: flatbuffers.NewBuilder(1024),
		allTags: NewSearchDataMap(),
	}
}

func (b *SearchPageBuilder) AddData(data *SearchEntryMutable) int {
	if data.Tags != nil {
		data.Tags.Range(func(k, v string) {
			b.allTags.Add(k, v)
		})
	}

	oldOffset := b.builder.Offset()
	offset := data.WriteToBuilder(b.builder)
	b.pageEntries = append(b.pageEntries, offset)

	// bytes written
	return int(offset - oldOffset)
}

func (b *SearchPageBuilder) Finish() []byte {
	// At this point all individual entries have been written
	// to the fb builder. Now we need to wrap them up in the final
	// batch object.

	// Create vector
	SearchPageStartEntriesVector(b.builder, len(b.pageEntries))
	for _, entry := range b.pageEntries {
		b.builder.PrependUOffsetT(entry)
	}
	entryVector := b.builder.EndVector(len(b.pageEntries))

	// Create batch-level tags
	tagOffset := b.allTags.WriteToBuilder(b.builder)

	// Write final batch object
	SearchPageStart(b.builder)
	SearchPageAddEntries(b.builder, entryVector)
	SearchPageAddTags(b.builder, tagOffset)
	batch := SearchPageEnd(b.builder)
	b.builder.Finish(batch)
	buf := b.builder.FinishedBytes()

	return buf
}

func (b *SearchPageBuilder) Reset() {
	b.builder.Reset()
	b.pageEntries = b.pageEntries[:0]
	b.allTags = NewSearchDataMap()
}

// Get searches the entry and returns the first value found for the given key.
func (s *SearchEntry) Get(k string) string {
	kv := &KeyValues{}
	kb := bytes.ToLower([]byte(k))

	// TODO - Use binary search since keys/values are sorted
	for i := 0; i < s.TagsLength(); i++ {
		s.Tags(kv, i)
		if bytes.Equal(kv.Key(), kb) {
			return string(kv.Value(0))
		}
	}

	return ""
}

// Contains returns true if the key and value are found in the search data.
// Buffer KeyValue object can be passed to reduce allocations. Key and value must be
// already converted to byte slices which match the nature of the flatbuffer data
// which reduces allocations even further.
func (s *SearchEntry) Contains(k []byte, v []byte, buffer *KeyValues) bool {
	return ContainsTag(s, buffer, k, v)
}

func (s *SearchEntry) Reset(b []byte) {
	n := flatbuffers.GetUOffsetT(b)
	s.Init(b, n)
}

func NewSearchEntryFromBytes(b []byte) *SearchEntry {
	return GetRootAsSearchEntry(b, 0)
}

type FBTagContainer interface {
	Tags(obj *KeyValues, j int) bool
	TagsLength() int
}

func ContainsTag(s FBTagContainer, kv *KeyValues, k []byte, v []byte) bool {

	kv = FindTag(s, kv, k)
	if kv != nil {
		// Linear search for matching values
		l := kv.ValueLength()
		for j := 0; j < l; j++ {
			if bytes.Contains(kv.Value(j), v) {
				return true
			}
		}
	}

	return false
}

func FindTag(s FBTagContainer, kv *KeyValues, k []byte) *KeyValues {

	idx := binarySearch(s.TagsLength(), func(i int) int {
		s.Tags(kv, i)
		// Note comparison here is backwards because KeyValues are written to flatbuffers in reverse order.
		return bytes.Compare(kv.Key(), k)
	})

	if idx >= 0 {
		// Data is left in buffer when matched
		return kv
	}

	return nil
}

// binarySearch that finds exact matching entry. Returns non-zero index when found, or -1 when not found
// Inspired by sort.Search but makes uses of tri-state comparator to eliminate the last comparison when
// we want to find exact match, not insertion point.
func binarySearch(n int, compare func(int) int) int {
	i, j := 0, n
	for i < j {
		h := int(uint(i+j) >> 1) // avoid overflow when computing h
		// i ≤ h < j
		switch compare(h) {
		case 0:
			// Found exact match
			return h
		case -1:
			j = h
		case 1:
			i = h + 1
		}
	}

	// No match
	return -1
}
