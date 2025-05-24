package src

import (
	"fmt"
	"math/rand"
	"sadl-racs/common"
	"sadl-racs/proto"
	"strconv"
	"time"
)

/*
	defines the data structures specific to Mem Blocks
*/

type MemPool struct {
	blockMap             MessageStore         //saves the blocks
	incomingBuffer       []*proto.ClientBatch // saves client batches that need to be added to a future  mem blocks
	lastTimeBlockCreated time.Time            // last time a mem block was created
	lastSentBlock        []int                // the last sent block to each replica
	lastSeenAck          []int                // the last seen ack from each replica
	indexCounter         int                  // to create unique block ids
	mode                 int                  // 1 if all to all broadcast, 2 if selective broadcast with back pressure
	window               int                  // window for the number of outstanding blocks awaiting acks

	lastCompletedRounds []int //array of N elements (N= number of replica) that keeps track of the last block for which at least n-f block-acks were collected, for each replica
	awaitingAcks        bool  //states whether this replica is waiting for block-acks
	startTime           time.Time
}

/*
	Initialize a new MemPool
*/

func InitMemPool(mode int, numReplicas int, debugLevel int, debugOn bool, window int) *MemPool {
	mmp := MemPool{
		blockMap:             MessageStore{},
		incomingBuffer:       make([]*proto.ClientBatch, 0),
		lastTimeBlockCreated: time.Now(),
		lastSentBlock:        make([]int, numReplicas),
		lastSeenAck:          make([]int, numReplicas),
		indexCounter:         1,
		mode:                 mode,
		window:               window,
		lastCompletedRounds:  make([]int, numReplicas),
		awaitingAcks:         false,
		startTime:            time.Now(),
	}

	for i := 0; i < numReplicas; i++ {
		mmp.lastSentBlock[i] = 0
		mmp.lastSeenAck[i] = 0
		mmp.lastCompletedRounds[i] = 0
	}

	mmp.blockMap.Init(debugLevel, debugOn)
	return &mmp
}

/*
Handler for MemPool Messages
*/
func (rp *Replica) handleMemPool(message *proto.MemPool) {

	if message.Type == 1 {
		//Mem-Pool-Mem-Block 1
		// save the mem block
		rp.memPool.blockMap.Add(message)
		// Set lastCompletedRounds[sender] to the parent blocks round
		parentId := message.ParentBlockId
		node, sequence := common.ExtractSequenceNumber(parentId)
		if node != rp.name {
			rp.memPool.lastCompletedRounds[node-1] = sequence
		}

		if rp.debugOn {
			common.Debug("Last Completed Rounds are "+fmt.Sprintf("%v", rp.memPool.lastCompletedRounds), 0, rp.debugLevel, rp.debugOn)
		}

		// send Mem-Pool-Mem-Block-Ack 2 to the sender
		memPoolAck := proto.MemPool{
			Sender:        rp.name,
			UniqueId:      message.UniqueId,
			Type:          2,
			RoundNumber:   message.RoundNumber,
			ParentBlockId: message.ParentBlockId,
			Creator:       message.Creator,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.MemPoolRPC,
			Obj:  &memPoolAck,
		}

		rp.sendMessage(message.Sender, rpcPair)
		if rp.debugOn {
			common.Debug("Sent Mem Pool Ack message with type 2 to "+strconv.Itoa(int(message.Sender)), 0, rp.debugLevel, rp.debugOn)
		}

	} else if message.Type == 2 {
		// Mem-Pool-Mem-Block-Ack 2
		_, sequence := common.ExtractSequenceNumber(message.UniqueId)
		if sequence > rp.memPool.lastSeenAck[message.Sender-1] {
			rp.memPool.lastSeenAck[message.Sender-1] = sequence
		}

		if message.UniqueId == strconv.Itoa(int(rp.name))+"."+strconv.Itoa(rp.memPool.indexCounter-1) && rp.memPool.awaitingAcks == true {
			rp.memPool.blockMap.AddAck(message.UniqueId, message.Sender)
			acks := rp.memPool.blockMap.GetAcks(message.UniqueId)
			if acks != nil && len(acks) == len(rp.replicaAddrList)/2+1 {
				rp.memPool.awaitingAcks = false
				rp.memPool.lastCompletedRounds[rp.name-1]++
				if rp.debugOn {
					common.Debug("Received n-f acks for the block "+message.UniqueId, 0, rp.debugLevel, rp.debugOn)
				}
			}
		}
	} else if message.Type == 3 {
		// Mem-Pool-Mem-Block-Request 3
		// if the request mem block exists, send a Mem-Pool-Mem-Block-Response 4 to the sender
		block, ok := rp.memPool.blockMap.Get(message.UniqueId)
		if ok {
			memPoolResponse := proto.MemPool{
				Sender:        rp.name,
				UniqueId:      block.UniqueId,
				Type:          4,
				Note:          block.Note,
				ClientBatches: block.ClientBatches,
				RoundNumber:   block.RoundNumber,
				ParentBlockId: block.ParentBlockId,
				Creator:       block.Creator,
			}

			rpcPair := common.RPCPair{
				Code: rp.messageCodes.MemPoolRPC,
				Obj:  &memPoolResponse,
			}

			rp.sendMessage(message.Sender, rpcPair)
			if rp.debugOn {
				common.Debug("Sent Mem Pool response message with type 4 to "+strconv.Itoa(int(message.Sender)), 0, rp.debugLevel, rp.debugOn)
			}
		}

	} else if message.Type == 4 {
		// Mem-Pool-Mem-Block-Response 4
		// save the block in the store
		rp.memPool.blockMap.Add(message)
		if rp.debugOn {
			common.Debug("Saved a mem block as an explicit response from "+strconv.Itoa(int(message.Sender)), 0, rp.debugLevel, rp.debugOn)
		}
	}

}

/*
	creates a new Mem block if the conditions for creating a new block is satisfied
		condition 1: incoming buffer is full || maximum time is passed
		condition 2: awaitingAcks is false
*/

func (rp *Replica) createNewMemBlock() {
	if (len(rp.memPool.incomingBuffer) >= rp.replicaBatchSize || (time.Now().Sub(rp.memPool.lastTimeBlockCreated).Microseconds() > int64(rp.replicaBatchTime) &&
		len(rp.memPool.incomingBuffer) > 0)) && rp.memPool.awaitingAcks == false {

		if rp.debugOn {
			common.Debug("Creating a new mem block ", 0, rp.debugLevel, rp.debugOn)
		}

		// create a new Mem block
		bParentId := strconv.Itoa(int(rp.name)) + "." + strconv.Itoa(rp.memPool.indexCounter-1) // because we always increase the index counter upon creating a new block

		acks := rp.memPool.blockMap.GetAcks(bParentId)

		if acks == nil || len(acks) < len(rp.replicaAddrList)/2+1 {
			return
		}

		var batches []*proto.ClientBatch

		if len(rp.memPool.incomingBuffer) <= rp.replicaBatchSize {
			batches = rp.memPool.incomingBuffer
			rp.memPool.incomingBuffer = make([]*proto.ClientBatch, 0)
		} else {
			batches = rp.memPool.incomingBuffer[:rp.replicaBatchSize]
			rp.memPool.incomingBuffer = rp.memPool.incomingBuffer[rp.replicaBatchSize:]
		}

		newMemBlock := proto.MemPool{
			Sender:        rp.name,
			UniqueId:      strconv.Itoa(int(rp.name)) + "." + strconv.Itoa(rp.memPool.indexCounter),
			Type:          1,
			Note:          "",
			ClientBatches: batches,
			RoundNumber:   int64(rp.memPool.indexCounter),
			ParentBlockId: bParentId,
			Creator:       rp.name,
		}

		// set awaitingAcks to true so that until this block gets n-f acks, no future mem blocks are created
		rp.memPool.awaitingAcks = true
		//	save the mem block in mem store
		rp.memPool.blockMap.Add(&newMemBlock)

		// create a replica array which contains the names of each replica

		replicas := make([]int32, len(rp.replicaAddrList))

		i := 0
		for name, _ := range rp.replicaAddrList {
			replicas[i] = name
			i++
		}

		if rp.isAsynchronous {

			epoch := time.Now().Sub(rp.memPool.startTime).Milliseconds() / int64(rp.timeEpochSize)

			if rp.amIAttacked(int(epoch)) {
				time.Sleep(time.Duration(rp.asynchronousTime) * time.Millisecond)
			}
		}

		//	if the mode is 1
		//		send the mem block to all replicas
		if rp.mode == 1 {
			rp.sendMemBlockToEveryone(&newMemBlock, replicas)
		}
		//	else if the mode is 2
		//		send the mem block to the best healthy replicas
		if rp.mode == 2 {
			rp.sendBlockToBestMajority(&newMemBlock, replicas)
		}

		//	increment the counter, and update time
		rp.memPool.indexCounter++
		rp.memPool.lastTimeBlockCreated = time.Now()
	}
}

/*
	Send a mem block to each replica in replicas
*/

func (rp *Replica) sendMemBlockToEveryone(m *proto.MemPool, replicas []int32) {

	for i := 0; i < len(replicas); i++ {
		replica := replicas[i]

		memPool := proto.MemPool{
			Sender:        m.Sender,
			UniqueId:      m.UniqueId,
			Type:          m.Type,
			Note:          m.Note,
			ClientBatches: m.ClientBatches,
			RoundNumber:   m.RoundNumber,
			ParentBlockId: m.ParentBlockId,
			Creator:       m.Creator,
		}

		rpcPair := common.RPCPair{
			Code: rp.messageCodes.MemPoolRPC,
			Obj:  &memPool,
		}

		rp.sendMessage(replica, rpcPair)
		_, sequence := common.ExtractSequenceNumber(m.UniqueId)
		rp.memPool.lastSentBlock[replica-1] = sequence

		if rp.debugOn {
			common.Debug("Sent Mem Pool message with type 1 to "+strconv.Itoa(int(replica)), 0, rp.debugLevel, rp.debugOn)
		}
	}
}

/*
	Back pressure based broadcasting where the sender sends only to healthy replicas
*/

func (rp *Replica) sendBlockToBestMajority(m *proto.MemPool, replicas []int32) {
	healthyReplicas := make([]int32, 0) // an array that has the names of healthy replicas
	healthyCount := 0
	threshold := -10

	for healthyCount < len(replicas)/2+1 {
		threshold += 10
		healthyReplicas = make([]int32, 0)
		healthyCount = 0
		for i := 0; i < len(replicas); i++ {
			if rp.memPool.lastSentBlock[replicas[i]-1]-rp.memPool.lastSeenAck[replicas[i]-1] < rp.memPool.window+threshold {
				healthyReplicas = append(healthyReplicas, replicas[i])
				healthyCount++
			}
		}
	}
	if len(replicas) != len(healthyReplicas) {
		if rp.debugOn {
			common.Debug("Selected a healthy replica set when threshold is "+strconv.Itoa(int(threshold))+" and the replicas are "+fmt.Sprintf("%v", healthyReplicas), 0, rp.debugLevel, rp.debugOn)
		}
	}
	rp.sendMemBlockToEveryone(m, healthyReplicas)
}

/*
	This method is invoked when the replica needs mem block to commit the block
	Randomly selects a replica and sends a Mem-Pool-Mem-Block-Request 3
*/

func (rp *Replica) sendExternalMemBlockRequest(id string) {

	randomReplica := rand.Intn(42)%rp.numReplicas + 1

	for randomReplica == int(rp.name) {
		randomReplica = rand.Intn(42)%rp.numReplicas + 1
	}

	externalMemBlockRequest := proto.MemPool{
		Sender:   rp.name,
		UniqueId: id,
		Type:     3,
	}

	rpcPair := common.RPCPair{
		Code: rp.messageCodes.MemPoolRPC,
		Obj:  &externalMemBlockRequest,
	}

	rp.sendMessage(int32(randomReplica), rpcPair)
	if rp.debugOn {
		common.Debug("Sent Mem Pool Mem block request message with type 3 to "+strconv.Itoa(randomReplica), 0, rp.debugLevel, rp.debugOn)
	}
}

// handler for new client batches

func (rp *Replica) handleClientBatch(batch *proto.ClientBatch) {
	//Upon receiving a client batch, save it in an internal pending buffer
	rp.memPool.incomingBuffer = append(rp.memPool.incomingBuffer, batch)
	rp.createNewMemBlock()
}

///*
//	Printing the mem store for debug purpose
//*/
//
//func (rp *Replica) printLogMemPool() {
//	f, err := os.Create(rp.logFilePath + strconv.Itoa(int(rp.name)) + "-mem-pool.txt")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer f.Close()
//
//	for memBlockID, memBlock := range rp.memPool.blockMap.MessageBlocks {
//		clientBatches := memBlock.MessageBlock.ClientBatches
//		for clientBatchIndex := 0; clientBatchIndex < len(clientBatches); clientBatchIndex++ {
//			clientBatch := clientBatches[clientBatchIndex]
//			clientBatchID := clientBatch.UniqueId
//			clientBatchCommands := clientBatch.Requests
//			for clientRequestIndex := 0; clientRequestIndex < len(clientBatchCommands); clientRequestIndex++ {
//				clientRequest := clientBatchCommands[clientRequestIndex].Command
//				_, _ = f.WriteString(memBlockID + "-" + clientBatchID + "-" + strconv.Itoa(clientRequestIndex) + ":" + clientRequest + "\n")
//			}
//		}
//
//	}
//
//}

/*
	send back the client response batches
*/

func (rp *Replica) sendMemPoolClientResponse(memPoolBlock *proto.MemPool) {
	clientBatches := memPoolBlock.ClientBatches
	for i := 0; i < len(clientBatches); i++ {

		if clientBatches[i].Sender != -1 {

			// send the response back to the client
			resClientBatch := proto.ClientBatch{
				UniqueId: clientBatches[i].UniqueId,
				Requests: clientBatches[i].Requests,
				Sender:   clientBatches[i].Sender,
			}

			rpcPair := common.RPCPair{
				Code: rp.messageCodes.ClientBatchRpc,
				Obj:  &resClientBatch,
			}

			rp.sendMessage(int32(resClientBatch.Sender), rpcPair)
		}
	}
}
