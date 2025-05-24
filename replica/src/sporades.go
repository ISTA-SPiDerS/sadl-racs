package src

import (
	"fmt"
	"log"
	"mandator-sporades/common"
	"mandator-sporades/proto"
	"math/rand"
	"os"
	"strconv"
	"time"
)

/*
	AsyncConsensus stores the data structures for the consensus
*/

type AsyncConsensus struct {
	vCurr           int32                              // current view number
	rCurr           int32                              // current round number
	blockHigh       *proto.AsyncConsensus_Block        // the block with the highest round number in which the node received a propose message
	blockCommit     *proto.AsyncConsensus_Block        // the last block to commit
	consensusPool   *AsyncConsensusStore               // an id of a consensus block is creator_name.v.r.type.level. type can be r (regular) or f (fallback), level can be 1,2 or -1 (for regular blocks)
	isAsync         bool                               // true if in fallback mode, and false if in sync mode
	bFall           map[string][]string                // far storing the fallback block ids: the key is view.level, value is the array of fallback block ids
	voteReplies     map[string][]*proto.AsyncConsensus // stores the vote messages received in the sync path. The key is v.r, the value is the array of received votes
	timeoutMessages map[int32][]*proto.AsyncConsensus  // stores the timeout messages for each view, the key is the view number, value is the array of received timeout messages
	viewTimer       *common.TimerWithCancel            // a timer to set the view timeouts in the acceptors

	lastCommittedBlock  *proto.AsyncConsensus_Block // the last committed block
	lastCommittedRounds []int                       // for each replica, the index of the mempool block that was last committed
	randomness          []int                       // predefined random leader node  name for each view

	sentLevel2Block   map[int32]bool // records if level 2 fallback block was sent for view v; key is v
	startTime         time.Time      // time when the consensus was started
	lastCommittedTime time.Time      // time when the last consensus block was committed
}

/*
	Init Async Consensus Data Structs
*/

func InitAsyncConsensus(debugLevel int, debugOn bool, numReplicas int) *AsyncConsensus {

	blockHigh := &proto.AsyncConsensus_Block{
		Id:       "genesis-block",
		V:        0,
		R:        0,
		Parent:   nil,
		Commands: nil,
		Level:    -1,
	}

	asyncConsensus := AsyncConsensus{
		vCurr:               0,
		rCurr:               0,
		blockHigh:           blockHigh,
		blockCommit:         blockHigh,
		consensusPool:       &AsyncConsensusStore{},
		isAsync:             false,
		bFall:               make(map[string][]string),
		voteReplies:         make(map[string][]*proto.AsyncConsensus),
		timeoutMessages:     make(map[int32][]*proto.AsyncConsensus),
		lastCommittedBlock:  blockHigh,
		lastCommittedRounds: make([]int, numReplicas),
		randomness:          make([]int, 0),
		sentLevel2Block:     make(map[int32]bool),
	}

	// initialize the consensus pool
	asyncConsensus.consensusPool.Init(debugLevel, debugOn)
	// add genesis block to the AsyncConsensusStore
	asyncConsensus.consensusPool.Add(blockHigh)

	// initialize the lastCommittedRounds
	for i := 0; i < numReplicas; i++ {
		asyncConsensus.lastCommittedRounds[i] = 0
	}

	//initialize randomness and sentLevel2Block; assumes 1000000 max view changes, increase the 1000000 if needed
	s2 := rand.NewSource(42)
	r2 := rand.New(s2)
	for i := 0; i < 1000000; i++ {
		asyncConsensus.randomness = append(asyncConsensus.randomness, (r2.Intn(42))%numReplicas+1)
		asyncConsensus.sentLevel2Block[int32(i)] = false
	}

	asyncConsensus.randomness[10] = 1 // initial leader is replica 1

	return &asyncConsensus
}

/*
	Returns the leader for the view; view leaders are pre-defined
*/

func (rp *Replica) getLeader(view int32) int32 {
	return int32(rp.asyncConsensus.randomness[view+10])
}

/*
	send the initial consensus vote message for the genesis block
*/

func (rp *Replica) sendGenesisConsensusVote() {
	rp.asyncConsensus.startTime = time.Now()
	rp.asyncConsensus.lastCommittedTime = time.Now()

	//send <vote, v cur , r cur , block high > to leader
	nextLeader := rp.getLeader(rp.asyncConsensus.vCurr)

	genesisBlock, ok := rp.asyncConsensus.consensusPool.Get("genesis-block")

	if !ok {
		panic("genesis consensus block not found")
	}

	bootStrapVote := proto.AsyncConsensus{
		Sender:      rp.name,
		Receiver:    nextLeader,
		UniqueId:    "",
		Type:        2,
		Note:        "",
		V:           rp.asyncConsensus.vCurr,
		R:           rp.asyncConsensus.rCurr,
		BlockHigh:   rp.makeGreatGrandParentNil(genesisBlock),
		BlockNew:    nil,
		BlockCommit: nil,
	}

	rpcPair := common.RPCPair{
		Code: rp.messageCodes.AsyncConsensus,
		Obj:  &bootStrapVote,
	}

	rp.sendMessage(nextLeader, rpcPair)
	if rp.debugOn {
		common.Debug("Sent boot strap consensus vote to "+strconv.Itoa(int(nextLeader)), 0, rp.debugLevel, rp.debugOn)
	}

	// start the timeout
	rp.setViewTimer()
}

/*
	Sets a timer, which once timeout will broadcast a timeout message
*/

func (rp *Replica) setViewTimer() {

	rp.asyncConsensus.viewTimer = common.NewTimerWithCancel(time.Duration(rp.viewTimeout) * time.Microsecond)
	vCurr := rp.asyncConsensus.vCurr
	rCurr := rp.asyncConsensus.rCurr
	rp.asyncConsensus.viewTimer.SetTimeoutFunction(func() {
		// this function runs in a seperate thread, hence we do not send timeout message in this function, instead send a timeout-internal signal
		internalTimeoutNotification := proto.AsyncConsensus{
			Sender:      rp.name,
			Receiver:    rp.name,
			UniqueId:    "",
			Type:        6,
			Note:        "",
			V:           vCurr,
			R:           rCurr,
			BlockHigh:   nil,
			BlockNew:    nil,
			BlockCommit: nil,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.AsyncConsensus,
			Obj:  &internalTimeoutNotification,
		}
		rp.sendMessage(rp.name, rpcPair)
		if rp.debugOn {
			common.Debug("Sent an internal timeout notification for view "+strconv.Itoa(int(rp.asyncConsensus.vCurr)), 4, rp.debugLevel, rp.debugOn)
		}

	})
	rp.asyncConsensus.viewTimer.Start()
}

/*
	Handler for the async consensus messages
*/

func (rp *Replica) handleAsyncConsensus(message *proto.AsyncConsensus) {

	debugLevel := 0

	if message.Type == 1 {
		if rp.debugOn {
			common.Debug("Received a propose message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusProposeSync(message)

	} else if message.Type == 2 {
		if rp.debugOn {
			common.Debug("Received a vote message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusVoteSync(message)

	} else if message.Type == 3 {
		if rp.debugOn {
			common.Debug("Received a timeout message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusTimeout(message)

	} else if message.Type == 4 {
		if rp.debugOn {
			common.Debug("Received a propose-async message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusProposeAsync(message)

	} else if message.Type == 5 {
		if rp.debugOn {
			common.Debug("Received a vote-async message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusAsyncVote(message)

	} else if message.Type == 6 {
		if rp.debugOn {
			common.Debug("Received a timeout-internal message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusInternalTimeout(message)
	} else if message.Type == 7 {
		if rp.debugOn {
			common.Debug("Received a consensus-external-request message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusExternalRequest(message)
	} else if message.Type == 8 {
		if rp.debugOn {
			common.Debug("Received a consensus-external-response message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusExternalResponseMessage(message)
	} else if message.Type == 9 {
		if rp.debugOn {
			common.Debug("Received a async fallback-complete message from "+strconv.Itoa(int(message.Sender))+
				" for view "+strconv.Itoa(int(message.V))+" for round "+strconv.Itoa(int(message.R))+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), debugLevel, rp.debugLevel, rp.debugOn)
		}
		rp.handleConsensusFallbackCompleteMessage(message)
	}

}

/*
	util function to compare the ranks; checks if v1;r1 rank higher than v2,r2
*/

func (rp *Replica) hasGreaterRank(v1 int32, r1 int32, v2 int32, r2 int32) bool {
	if v1 > v2 {
		return true
	}
	if v1 == v2 && r1 > r2 {
		return true
	}
	return false
}

/*
	Util function to compare the ranks; checks if v1,r1 >= v2,r2
*/

func (rp *Replica) hasGreaterThanOrEqualRank(v1 int32, r1 int32, v2 int32, r2 int32) bool {
	if v1 > v2 {
		return true
	}
	if v1 == v2 && r1 > r2 {
		return true
	}
	if v1 == v2 && r1 == r2 {
		return true
	}
	return false
}

/*
	Util function to extract the highest blockHigh from the received set of vote/timeout messages
*/

func (rp *Replica) extractHighestRankedBlockHigh(messages []*proto.AsyncConsensus) *proto.AsyncConsensus_Block {
	highBlock := messages[0].BlockHigh
	for i := 0; i < len(messages); i++ {
		if rp.hasGreaterRank(messages[i].BlockHigh.V, messages[i].BlockHigh.R, highBlock.V, highBlock.R) {
			highBlock = messages[i].BlockHigh
		}
	}
	return highBlock
}

/*
	Util function to convert int[] to int32[]
*/

func (rp *Replica) convertToInt32Array(elements []int) []int32 {
	returnArray := make([]int32, 0)
	for i := 0; i < len(elements); i++ {
		returnArray = append(returnArray, int32(elements[i]))
	}
	return returnArray
}

/*
	This method is invoked when the replica needs a consensus block
	Randomly selects a replica and sends a 7-consensus-external-request
*/

func (rp *Replica) sendExternalConsensusRequest(id string) {

	randomReplica := rand.Intn(42)%rp.numReplicas + 1

	for randomReplica == int(rp.name) {
		randomReplica = rand.Intn(42)%rp.numReplicas + 1
	}

	externalConsensusRequest := proto.AsyncConsensus{
		Sender:      rp.name,
		Receiver:    int32(randomReplica),
		UniqueId:    id,
		Type:        7,
		Note:        "",
		V:           -1,
		R:           -1,
		BlockHigh:   nil,
		BlockNew:    nil,
		BlockCommit: nil,
	}

	rpcPair := common.RPCPair{
		Code: rp.messageCodes.AsyncConsensus,
		Obj:  &externalConsensusRequest,
	}

	rp.sendMessage(int32(randomReplica), rpcPair)
	if rp.debugOn {
		common.Debug("Sent external consensus request message with type 7 to "+strconv.Itoa(randomReplica), 1, rp.debugLevel, rp.debugOn)
	}
}

/*
	Handler for external consensus request messages
*/

func (rp *Replica) handleConsensusExternalRequest(message *proto.AsyncConsensus) {
	// if the consensus block with id message.unique_id exists in the consensus store
	block, ok := rp.asyncConsensus.consensusPool.Get(message.UniqueId)
	if ok {
		// send an external consensus response 8 message to the sender
		externalConsensusResponse := proto.AsyncConsensus{
			Sender:      rp.name,
			Receiver:    message.Sender,
			UniqueId:    message.UniqueId,
			Type:        8,
			Note:        "",
			V:           -1,
			R:           -1,
			BlockHigh:   nil,
			BlockNew:    rp.makeGreatGrandParentNil(block),
			BlockCommit: nil,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.AsyncConsensus,
			Obj:  &externalConsensusResponse,
		}

		rp.sendMessage(message.Sender, rpcPair)
		if rp.debugOn {
			common.Debug("Sent external consensus response message with type 8 to "+strconv.Itoa(int(message.Sender)), 1, rp.debugLevel, rp.debugOn)
		}
	}
}

/*
	Handler for external consensus response messages
*/

func (rp *Replica) handleConsensusExternalResponseMessage(message *proto.AsyncConsensus) {
	rp.asyncConsensus.consensusPool.Add(message.BlockNew)
	if rp.debugOn {
		common.Debug("Added a consensus block from an external response", 1, rp.debugLevel, rp.debugOn)
	}
}

/*
	remove parent block
*/

func (rp *Replica) removeParentBlock(blockOri *proto.AsyncConsensus_Block) *proto.AsyncConsensus_Block {
	returnBlock := &proto.AsyncConsensus_Block{
		Id:       blockOri.Id,
		V:        blockOri.V,
		R:        blockOri.R,
		Parent:   nil,
		Commands: blockOri.Commands,
		Level:    blockOri.Level,
	}
	return returnBlock
}

/*
	copy block
*/

func (rp *Replica) copyBlock(blockOri *proto.AsyncConsensus_Block) *proto.AsyncConsensus_Block {
	returnBlock := &proto.AsyncConsensus_Block{
		Id:       blockOri.Id,
		V:        blockOri.V,
		R:        blockOri.R,
		Parent:   blockOri.Parent,
		Commands: blockOri.Commands,
		Level:    blockOri.Level,
	}
	return returnBlock
}

/*
	Set the great-grandparent element to nil and return a new copy of the block
*/

func (rp *Replica) makeGreatGrandParentNil(blockOri *proto.AsyncConsensus_Block) *proto.AsyncConsensus_Block {

	block := blockOri

	if block.Parent != nil && block.Parent.Parent != nil && block.Parent.Parent.Parent != nil && block.Parent.Parent.Parent.Parent != nil {
		newGreatGrandParent := rp.removeParentBlock(block.Parent.Parent.Parent)
		newGrandParent := rp.copyBlock(block.Parent.Parent)
		newGrandParent.Parent = newGreatGrandParent
		newGrandParent = rp.copyBlock(newGrandParent)
		newParent := rp.copyBlock(block.Parent)
		newParent.Parent = newGrandParent
		newParent = rp.copyBlock(newParent)
		newBlock := rp.copyBlock(block)
		newBlock.Parent = newParent
		return newBlock
	} else {
		return blockOri
	}
}

/*
	Printing the replicated log for testing purposes
*/

func (rp *Replica) printLogConsensus() {
	f, err := os.Create(rp.logFilePath + strconv.Itoa(int(rp.name)) + "-consensus.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	head := rp.asyncConsensus.blockCommit // the last block to commit
	if head == nil {
		return
	}
	genesisBlock, ok := rp.asyncConsensus.consensusPool.Get("genesis-block")
	if !ok {
		panic("Genesis Block not found when printing the logs")
	}
	//toCommit = [] contains all the entries from the genesisBlock (not including) to rp.blockCommit (included)
	toCommit := make([]*proto.AsyncConsensus_Block, 0)

	for head.Id != genesisBlock.Id {
		//	toCommit.append(head)
		toCommit = append([]*proto.AsyncConsensus_Block{head}, toCommit...)
		//	head = head.parent
		head = head.Parent

		//	if head is in the consensus pool
		headBlock, ok := rp.asyncConsensus.consensusPool.Get(head.Id)
		if ok {
			head = headBlock
		} else {
			panic("Consensus block " + head.Id + " not found in the pool")
		}

	}

	lastCommittedMemPoolIndexes := make([]int, rp.numReplicas)
	for i := 0; i < rp.numReplicas; i++ {
		lastCommittedMemPoolIndexes[i] = 0
	}

	for i := 0; i < len(toCommit); i++ {
		nextBlockToCommit := toCommit[i] // toCommit[i] is the next block to be committed
		nextMemBlockLogPositionsToCommit := nextBlockToCommit.Commands

		// for each log position in nextMemBlockLogPositionsToCommit that corresponds to different replicas, check if the index is --
		// greater than the lastCommittedMemPoolIndexes
		for j := 0; j < rp.numReplicas; j++ {
			if int(nextMemBlockLogPositionsToCommit[j]) > lastCommittedMemPoolIndexes[j] {
				// there are new entries to commit for this index
				startMemPoolCounter := lastCommittedMemPoolIndexes[j] + 1
				lastMemPoolCounter := int(nextMemBlockLogPositionsToCommit[j])

				for k := startMemPoolCounter; k <= lastMemPoolCounter; k++ {
					memPoolName := strconv.Itoa(j+1) + "." + strconv.Itoa(k)
					memBlock, ok := rp.memPool.blockMap.Get(memPoolName)
					if !ok {
						panic("memblock " + memPoolName + " not found")
					}
					for clientBatchIndex := 0; clientBatchIndex < len(memBlock.ClientBatches); clientBatchIndex++ {
						clientBatch := memBlock.ClientBatches[clientBatchIndex]
						clientBatchID := clientBatch.UniqueId
						clientBatchCommands := clientBatch.Requests
						for clientRequestIndex := 0; clientRequestIndex < len(clientBatchCommands); clientRequestIndex++ {
							clientRequest := clientBatchCommands[clientRequestIndex].Command
							_, _ = f.WriteString(nextBlockToCommit.Id + "-" + memPoolName + "-" + clientBatchID + "-" + strconv.Itoa(clientRequestIndex) + ":" + clientRequest + "\n")
						}
					}

				}
				lastCommittedMemPoolIndexes[j] = lastMemPoolCounter
			}
		}
	}

}
