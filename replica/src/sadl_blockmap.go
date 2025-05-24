package src

import (
	"mandator-sporades/common"
	"mandator-sporades/proto"
)

/*
	implement a block store for the mem pool
*/

/*
	a single element in the map is a mem-block with its set of acks
*/

type MemBlock struct {
	MessageBlock *proto.MemPool
	acks         []int32 // contains the set of nodes who acknowledged the mem block, a simple counter might work, but having an array is extensible
}

/*
	Message store defines the underlying map
*/

type MessageStore struct {
	MessageBlocks map[string]MemBlock
	debugLevel    int
	debugOn       bool
}

/*
	allocate the map object
*/

func (ms *MessageStore) Init(debugLevel int, debugOn bool) {
	ms.MessageBlocks = make(map[string]MemBlock)
	ms.debugLevel = debugLevel
	ms.debugOn = debugOn
	if ms.debugOn {
		common.Debug("Initialized a new mem block message store", 0, ms.debugLevel, ms.debugOn)
	}
}

/*
	Add a new  block to the store if it is not already there
*/

func (ms *MessageStore) Add(block *proto.MemPool) {
	_, ok := ms.MessageBlocks[block.UniqueId]
	if !ok {
		ms.MessageBlocks[block.UniqueId] = MemBlock{
			MessageBlock: block,
			acks:         make([]int32, 0),
		}
		if ms.debugOn {
			common.Debug("Added a new block to store with id "+block.UniqueId, 0, ms.debugLevel, ms.debugOn)
		}
	}

}

/*
	return an existing block
*/

func (ms *MessageStore) Get(id string) (*proto.MemPool, bool) {
	block, ok := ms.MessageBlocks[id]
	if !ok {
		if ms.debugOn {
			common.Debug("Requested mem block does not exist, hence returning nil for id "+id, 0, ms.debugLevel, ms.debugOn)
		}
		return nil, ok
	} else {
		if ms.debugOn {
			common.Debug("Requested mem block exists, hence returning the block for id "+id, 0, ms.debugLevel, ms.debugOn)
		}
		return block.MessageBlock, ok
	}
}

/*
	return the set of acks for a given block
*/

func (ms *MessageStore) GetAcks(id string) []int32 {
	block, ok := ms.MessageBlocks[id]
	if ok {
		if ms.debugOn {
			common.Debug("Requested mem block exists, hence returning the acks for id "+id, 0, ms.debugLevel, ms.debugOn)
		}
		return block.acks
	}
	if ms.debugOn {
		common.Debug("Requested mem block does not exist, hence returning nil acks for id "+id, 0, ms.debugLevel, ms.debugOn)
	}
	return nil
}

/*
	Remove an element from the map
*/

func (ms *MessageStore) Remove(id string) {
	delete(ms.MessageBlocks, id)
	if ms.debugOn {
		common.Debug("Removed a mem block with id "+id, 0, ms.debugLevel, ms.debugOn)
	}
}

/*
	add a new ack to the acks of a given block
*/

func (ms *MessageStore) AddAck(id string, node int32) {
	block, ok := ms.MessageBlocks[id]
	if ok {
		tempAcks := block.acks
		tempBlock := block.MessageBlock
		tempAcks = append(tempAcks, node)
		ms.Remove(id)
		ms.MessageBlocks[id] = MemBlock{
			MessageBlock: tempBlock,
			acks:         tempAcks,
		}
		if ms.debugOn {
			common.Debug("Added an ack for mem block with id "+id, 0, ms.debugLevel, ms.debugOn)
		}
	} else {
		if ms.debugOn {
			common.Debug("Adding an ack failed for mem block with id "+id+" because the mem block does not exist", 0, ms.debugLevel, ms.debugOn)
		}
	}
}
