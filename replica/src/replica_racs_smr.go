package src

import (
	"fmt"
	"sadl-racs/common"
	"sadl-racs/proto"
	"strconv"
	"time"
)

/*
	Commits all the entries from the last committed block +1 to blockCommit
*/

func (rp *Replica) updateSMR() {

	head := rp.asyncConsensus.blockCommit // the last block to commit
	if head == nil {
		return
	}
	//toCommit contains all the blocks from the lastCommittedBlock (not including) to rp.blockCommit (included)
	toCommit := make([]*proto.AsyncConsensus_Block, 0)

	for head.Id != rp.asyncConsensus.lastCommittedBlock.Id {

		if head.Parent == nil {
			panic("Consensus block " + head.Id + "'s parent does not exist")
		}

		//	toCommit.append(head)
		toCommit = append([]*proto.AsyncConsensus_Block{head}, toCommit...)

		//	head = head.parent
		head = head.Parent
		//	genesis block doesn't have a parent
		if head.Id == "genesis-block" {
			break
		}
		//	if head is in the consensus pool
		headBlock, ok := rp.asyncConsensus.consensusPool.Get(head.Id)
		if ok {
			head = headBlock
		} else {
			// request the consensus block from some random peer
			if rp.debugOn {
				common.Debug("Consensus block "+head.Id+" does not exist, sending an external request", 0, rp.debugLevel, rp.debugOn)
			}
			rp.sendExternalConsensusRequest(head.Id)
			return // because we are missing the blocks in the history
		}

	}

	// if the code comes to this place, then we have all the consensus blocks from lastCommittedBlock (not included) to blockCommit (included)

	//if there is nothing to commit
	if len(toCommit) == 0 {
		if rp.debugOn {
			common.Debug("There is nothing to commit", 1, rp.debugLevel, rp.debugOn)
		}
		return
	}

	// check if all the required mem blocks exist.
	// we don't return as soon as we find a missing block, instead, we iterate everything and send external requests

	readyToCommit := true // indicates whether we have all types of blocks required to commit the causal history

	for i := 0; i < len(toCommit); i++ {
		nextBlockToCommit := toCommit[i]
		nextMemBlockLogPositionsToCommit := nextBlockToCommit.Commands

		// for each log position in nextMemBlockLogPositionsToCommit that corresponds to different replicas, check if --
		// -- the index is greater than the last committed index of the replica's mem blocks
		for j := 0; j < rp.numReplicas; j++ {
			if int(nextMemBlockLogPositionsToCommit[j]) > rp.asyncConsensus.lastCommittedRounds[j] {
				// there are new memblocks to commit for this index
				startMemPoolCounter := rp.asyncConsensus.lastCommittedRounds[j] + 1
				lastMemPoolCounter := int(nextMemBlockLogPositionsToCommit[j])
				// for each mem block in the range startMemPoolCounter to lastMemPoolCounter check if the block exists, if not send an external mem pool request.
				// if anything is missing, mark the ready to commit as false
				for k := startMemPoolCounter; k <= lastMemPoolCounter; k++ {
					memPoolName := strconv.Itoa(j+1) + "." + strconv.Itoa(k)
					_, ok := rp.memPool.blockMap.Get(memPoolName)
					if !ok {
						if rp.debugOn {
							common.Debug("Mem block with id "+memPoolName+" does not exist, sending an external request", 0, rp.debugLevel, rp.debugOn)
						}
						rp.sendExternalMemBlockRequest(memPoolName)
						readyToCommit = false
					}
				}

			}
		}
	}

	if readyToCommit {
		// we have every blocks needed to commit
		for i := 0; i < len(toCommit); i++ {
			nextBlockToCommit := toCommit[i] // toCommit[i] is the next block to be committed
			nextMemBlockLogPositionsToCommit := nextBlockToCommit.Commands

			// for each log position in nextMemBlockLogPositionsToCommit that corresponds to different replicas, check if the index is greater than the last committed index
			for j := 0; j < rp.numReplicas; j++ {
				if int(nextMemBlockLogPositionsToCommit[j]) > rp.asyncConsensus.lastCommittedRounds[j] {
					// there are new entries to commit for this index
					startMemPoolCounter := rp.asyncConsensus.lastCommittedRounds[j] + 1
					lastMemPoolCounter := int(nextMemBlockLogPositionsToCommit[j])

					for k := startMemPoolCounter; k <= lastMemPoolCounter; k++ {
						memPoolName := strconv.Itoa(j+1) + "." + strconv.Itoa(k)
						memBlock, _ := rp.memPool.blockMap.Get(memPoolName)
						memPoolClientResponse := rp.updateApplicationLogic(memBlock)
						if rp.debugOn {
							common.Debug("Committed mem block "+memBlock.UniqueId, 1, rp.debugLevel, rp.debugOn)
						}

						rp.sendMemPoolClientResponse(memPoolClientResponse)

						if rp.debugOn {
							common.Debug("Committed mem block "+memPoolName, 1, rp.debugLevel, rp.debugOn)
						}
					}
					rp.asyncConsensus.lastCommittedRounds[j] = lastMemPoolCounter
				}
			}
			if rp.debugOn {
				common.Debug("Committed async consensus block "+nextBlockToCommit.Id+" with mem pool indexes "+fmt.Sprintf("%v", nextBlockToCommit.Commands)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.asyncConsensus.startTime)), 42, rp.debugLevel, rp.debugOn)
			}
			rp.asyncConsensus.lastCommittedBlock = nextBlockToCommit
			rp.asyncConsensus.consensusPool.Add(nextBlockToCommit)
			rp.asyncConsensus.lastCommittedTime = time.Now()
		}
	}

}

/*
	A generic application handler for processing a mini block, and forming the mini block response
*/

func (rp *Replica) updateApplicationLogic(memBlock *proto.MemPool) *proto.MemPool {
	return rp.state.Execute(memBlock)
}
