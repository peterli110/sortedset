// Copyright (c) 2016, Jerry.Wang
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// * Redistributions of source code must retain the above copyright notice, this
//  list of conditions and the following disclaimer.
//
// * Redistributions in binary form must reproduce the above copyright notice,
//  this list of conditions and the following disclaimer in the documentation
//  and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
// AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package sortedset

import (
	"math/rand"
)

// SCORE type score
type SCORE int64 // the type of score

const (
	// SkiplistMaxlevel Should be enough for 2^32 elements
	SkiplistMaxlevel = 32
	// SkiplistP Skiplist P = 1/4
	SkiplistP = 0.25
)

// SortedSet type of set
type SortedSet struct {
	header *Node
	tail   *Node
	length int64
	level  int
	dict   map[string]*Node
}

func createNode(level int, score SCORE, key string, value interface{}) *Node {
	node := Node{
		score: score,
		key:   key,
		Value: value,
		level: make([]Level, level),
	}
	return &node
}

// Returns a random level for the new skiplist node we are going to create.
// The return value of this function is between 1 and SKIPLIST_MAXLEVEL
// (both inclusive), with a powerlaw-alike distribution where higher
// levels are less likely to be returned.
func randomLevel() int {
	level := 1
	for float64(rand.Int31()&0xFFFF) < float64(SkiplistP*0xFFFF) {
		level++
	}
	if level < SkiplistMaxlevel {
		return level
	}

	return SkiplistMaxlevel
}

func (s *SortedSet) insertNode(score SCORE, key string, value interface{}) *Node {
	var update [SkiplistMaxlevel]*Node
	var rank [SkiplistMaxlevel]int64

	x := s.header
	for i := s.level - 1; i >= 0; i-- {
		/* store rank that is crossed to reach the insert position */
		if s.level-1 == i {
			rank[i] = 0
		} else {
			rank[i] = rank[i+1]
		}

		for x.level[i].forward != nil &&
			(x.level[i].forward.score < score ||
				(x.level[i].forward.score == score && // score is the same but the key is different
					x.level[i].forward.key < key)) {
			rank[i] += x.level[i].span
			x = x.level[i].forward
		}
		update[i] = x
	}

	/* we assume the key is not already inside, since we allow duplicated
	 * scores, and the re-insertion of score and redis object should never
	 * happen since the caller of Insert() should test in the hash table
	 * if the element is already inside or not. */
	level := randomLevel()

	if level > s.level { // add a new level
		for i := s.level; i < level; i++ {
			rank[i] = 0
			update[i] = s.header
			update[i].level[i].span = s.length
		}
		s.level = level
	}

	x = createNode(level, score, key, value)
	for i := 0; i < level; i++ {
		x.level[i].forward = update[i].level[i].forward
		update[i].level[i].forward = x

		/* update span covered by update[i] as x is inserted here */
		x.level[i].span = update[i].level[i].span - (rank[0] - rank[i])
		update[i].level[i].span = (rank[0] - rank[i]) + 1
	}

	/* increment span for untouched levels */
	for i := level; i < s.level; i++ {
		update[i].level[i].span++
	}

	if update[0] == s.header {
		x.backward = nil
	} else {
		x.backward = update[0]
	}
	if x.level[0].forward != nil {
		x.level[0].forward.backward = x
	} else {
		s.tail = x
	}
	s.length++
	return x
}

/* Internal function used by delete, DeleteByScore and DeleteByRank */
func (s *SortedSet) deleteNode(x *Node, update [SkiplistMaxlevel]*Node) {
	for i := 0; i < s.level; i++ {
		if update[i].level[i].forward == x {
			update[i].level[i].span += x.level[i].span - 1
			update[i].level[i].forward = x.level[i].forward
		} else {
			update[i].level[i].span--
		}
	}
	if x.level[0].forward != nil {
		x.level[0].forward.backward = x.backward
	} else {
		s.tail = x.backward
	}
	for s.level > 1 && s.header.level[s.level-1].forward == nil {
		s.level--
	}
	s.length--
	delete(s.dict, x.key)
}

/* Delete an element with matching score/key from the skiplist. */
func (s *SortedSet) delete(score SCORE, key string) bool {
	var update [SkiplistMaxlevel]*Node

	x := s.header
	for i := s.level - 1; i >= 0; i-- {
		for x.level[i].forward != nil &&
			(x.level[i].forward.score < score ||
				(x.level[i].forward.score == score &&
					x.level[i].forward.key < key)) {
			x = x.level[i].forward
		}
		update[i] = x
	}
	/* We may have multiple elements with the same score, what we need
	 * is to find the element with both the right score and object. */
	x = x.level[0].forward
	if x != nil && score == x.score && x.key == key {
		s.deleteNode(x, update)
		// free x
		return true
	}
	return false /* not found */
}

// New Create a new SortedSet
func New() *SortedSet {
	sortedSet := SortedSet{
		level: 1,
		dict:  make(map[string]*Node),
	}
	sortedSet.header = createNode(SkiplistMaxlevel, 0, "", nil)
	return &sortedSet
}

// GetCount Get the number of elements
func (s *SortedSet) GetCount() int {
	return int(s.length)
}

// PeekMin get the element with minimum score, nil if the set is empty
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) PeekMin() *Node {
	return s.header.level[0].forward
}

// PopMin get and remove the element with minimal score, nil if the set is empty
//
// // Time complexity of this method is : O(log(N))
func (s *SortedSet) PopMin() *Node {
	x := s.header.level[0].forward
	if x != nil {
		s.Remove(x.key)
	}
	return x
}

// PeekMax get the element with maximum score, nil if the set is empty
// Time Complexity : O(1)
func (s *SortedSet) PeekMax() *Node {
	return s.tail
}

// PopMax get and remove the element with maximum score, nil if the set is empty
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) PopMax() *Node {
	x := s.tail
	if x != nil {
		s.Remove(x.key)
	}
	return x
}

// AddOrUpdate Add an element into the sorted set with specific key / value / score.
// if the element is added, this method returns true; otherwise false means updated
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) AddOrUpdate(key string, score SCORE, value interface{}) bool {
	var newNode *Node

	found := s.dict[key]
	if found != nil {
		// score does not change, only update value
		if found.score == score {
			found.Value = value
		} else { // score changes, delete and re-insert
			s.delete(found.score, found.key)
			newNode = s.insertNode(score, key, value)
		}
	} else {
		newNode = s.insertNode(score, key, value)
	}

	if newNode != nil {
		s.dict[key] = newNode
	}
	return found == nil
}

// IncrBy increase an element into the sorted set with specific key / value / score.
// if the element is added, this method returns true; otherwise false means updated
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) IncrBy(key string, score SCORE, value interface{}) bool {
	var newNode *Node

	found := s.dict[key]
	if found != nil {
		// score changes, delete and re-insert
		newScore := found.score + score
		s.delete(found.score, found.key)
		newNode = s.insertNode(newScore, key, value)
	} else {
		newNode = s.insertNode(score, key, value)
	}

	if newNode != nil {
		s.dict[key] = newNode
	}
	return found != nil
}

// Remove Delete element specified by key
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) Remove(key string) *Node {
	found := s.dict[key]
	if found != nil {
		s.delete(found.score, found.key)
		return found
	}
	return nil
}

// GetByScoreRangeOptions options of get by range
type GetByScoreRangeOptions struct {
	Limit        int  // limit the max nodes to return
	ExcludeStart bool // exclude start value, so it search in interval (start, end] or (start, end)
	ExcludeEnd   bool // exclude end value, so it search in interval [start, end) or (start, end)
}

// GetByScoreRange Get the nodes whose score within the specific range
//
// If options is nil, it searchs in interval [start, end] without any limit by default
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) GetByScoreRange(start SCORE, end SCORE, options *GetByScoreRangeOptions) []*Node {

	// prepare parameters
	var limit = int(2147483648)
	if options != nil && options.Limit > 0 {
		limit = options.Limit
	}

	excludeStart := options != nil && options.ExcludeStart
	excludeEnd := options != nil && options.ExcludeEnd
	reverse := start > end
	if reverse {
		start, end = end, start
		excludeStart, excludeEnd = excludeEnd, excludeStart
	}

	//////////////////////////
	var nodes []*Node

	//determine if out of range
	if s.length == 0 {
		return nodes
	}
	//////////////////////////

	if reverse { // search from end to start
		x := s.header

		if excludeEnd {
			for i := s.level - 1; i >= 0; i-- {
				for x.level[i].forward != nil &&
					x.level[i].forward.score < end {
					x = x.level[i].forward
				}
			}
		} else {
			for i := s.level - 1; i >= 0; i-- {
				for x.level[i].forward != nil &&
					x.level[i].forward.score <= end {
					x = x.level[i].forward
				}
			}
		}

		for x != nil && limit > 0 {
			if excludeStart {
				if x.score <= start {
					break
				}
			} else {
				if x.score < start {
					break
				}
			}

			next := x.backward

			nodes = append(nodes, x)
			limit--

			x = next
		}
	} else {
		// search from start to end
		x := s.header
		if excludeStart {
			for i := s.level - 1; i >= 0; i-- {
				for x.level[i].forward != nil &&
					x.level[i].forward.score <= start {
					x = x.level[i].forward
				}
			}
		} else {
			for i := s.level - 1; i >= 0; i-- {
				for x.level[i].forward != nil &&
					x.level[i].forward.score < start {
					x = x.level[i].forward
				}
			}
		}

		/* Current node is the last with score < or <= start. */
		x = x.level[0].forward

		for x != nil && limit > 0 {
			if excludeEnd {
				if x.score >= end {
					break
				}
			} else {
				if x.score > end {
					break
				}
			}

			next := x.level[0].forward

			nodes = append(nodes, x)
			limit--

			x = next
		}
	}

	return nodes
}

// GetByRankRange Get nodes within specific rank range [start, end]
// Note that the rank is 1-based integer. Rank 1 means the first node; Rank -1 means the last node;
//
// If start is greater than end, the returned array is in reserved order
// If remove is true, the returned nodes are removed
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) GetByRankRange(start int, end int, remove bool) []*Node {

	/* Sanitize indexes. */
	if start < 0 {
		start = int(s.length) + start + 1
	}
	if end < 0 {
		end = int(s.length) + end + 1
	}
	if start <= 0 {
		start = 1
	}
	if end <= 0 {
		end = 1
	}

	reverse := start > end
	if reverse { // swap start and end
		start, end = end, start
	}

	var update [SkiplistMaxlevel]*Node
	var nodes []*Node
	var traversed = int(0)

	x := s.header
	for i := s.level - 1; i >= 0; i-- {
		for x.level[i].forward != nil &&
			traversed+int(x.level[i].span) < start {
			traversed += int(x.level[i].span)
			x = x.level[i].forward
		}
		if remove {
			update[i] = x
		} else {
			if traversed+1 == start {
				break
			}
		}
	}

	traversed++
	x = x.level[0].forward
	for x != nil && traversed <= end {
		next := x.level[0].forward

		nodes = append(nodes, x)

		if remove {
			s.deleteNode(x, update)
		}

		traversed++
		x = next
	}

	if reverse {
		for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
			nodes[i], nodes[j] = nodes[j], nodes[i]
		}
	}
	return nodes
}

// GetByRank Get node by rank.
// Note that the rank is 1-based integer. Rank 1 means the first node; Rank -1 means the last node;
//
// If remove is true, the returned nodes are removed
// If node is not found at specific rank, nil is returned
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) GetByRank(rank int, remove bool) *Node {
	nodes := s.GetByRankRange(rank, rank, remove)
	if len(nodes) == 1 {
		return nodes[0]
	}
	return nil
}

// GetByKey Get node by key
//
// If node is not found, nil is returned
// Time complexity : O(1)
func (s *SortedSet) GetByKey(key string) *Node {
	return s.dict[key]
}

// FindRank Find the rank of the node specified by key
// Note that the rank is 1-based integer. Rank 1 means the first node
//
// If the node is not found, 0 is returned. Otherwise rank(> 0) is returned
//
// Time complexity of this method is : O(log(N))
func (s *SortedSet) FindRank(key string) int {
	var rank = int(0)
	node := s.dict[key]
	if node != nil {
		x := s.header
		for i := s.level - 1; i >= 0; i-- {
			for x.level[i].forward != nil &&
				(x.level[i].forward.score < node.score ||
					(x.level[i].forward.score == node.score &&
						x.level[i].forward.key <= node.key)) {
				rank += int(x.level[i].span)
				x = x.level[i].forward
			}

			if x.key == key {
				return rank
			}
		}
	}
	return 0
}
