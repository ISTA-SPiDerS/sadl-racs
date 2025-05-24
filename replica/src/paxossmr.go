package src

import (
	"fmt"
	"mandator-sporades/common"
	"strconv"
	"time"
)

/*
	update SMR logic
*/

func (rp *Replica) updatePaxosSMR() {
	// since this method is called only when the last decided choice +1 is committed, we have to commit from last committed index + 1 to last decided index (included)
	readyToCommit := true

	for i := rp.paxosConsensus.lastCommittedLogIndex + 1; i < rp.paxosConsensus.lastDecidedLogIndex+1; i++ {
		nextInstanceToCommit := rp.paxosConsensus.replicatedLog[i]
		nextMemBlockLogPositionsToCommit := nextInstanceToCommit.decisions

		// for each log position in nextMemBlockLogPositionsToCommit that corresponds to different replicas, check if --
		// -- the index is greater than the last committed index of the replica's mem blocks
		for j := 0; j < rp.numReplicas; j++ {
			if len(nextMemBlockLogPositionsToCommit) < rp.numReplicas || len(rp.paxosConsensus.lastCommittedRounds) < rp.numReplicas {
				panic("instance with problematic decisions " + fmt.Sprintf("%v", nextInstanceToCommit) + " while last committed rounds " + fmt.Sprintf("%v", rp.paxosConsensus.lastCommittedRounds))
			}
			if int(nextMemBlockLogPositionsToCommit[j]) > rp.paxosConsensus.lastCommittedRounds[j] {
				// there are new memblocks to commit for this index
				startMemPoolCounter := rp.paxosConsensus.lastCommittedRounds[j] + 1
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
		// we have every mem blocks needed to commit
		for i := rp.paxosConsensus.lastCommittedLogIndex + 1; i < rp.paxosConsensus.lastDecidedLogIndex+1; i++ {
			nextInstanceToCommit := rp.paxosConsensus.replicatedLog[i]
			nextMemBlockLogPositionsToCommit := nextInstanceToCommit.decisions
			// for each log position in nextMemBlockLogPositionsToCommit that corresponds to different replicas, check if the index is greater than the last committed index
			for j := 0; j < rp.numReplicas; j++ {
				if int(nextMemBlockLogPositionsToCommit[j]) > rp.paxosConsensus.lastCommittedRounds[j] {
					// there are new entries to commit for this index
					startMemPoolCounter := rp.paxosConsensus.lastCommittedRounds[j] + 1
					lastMemPoolCounter := int(nextMemBlockLogPositionsToCommit[j])

					for k := startMemPoolCounter; k <= lastMemPoolCounter; k++ {
						memPoolName := strconv.Itoa(j+1) + "." + strconv.Itoa(k)
						memBlock, _ := rp.memPool.blockMap.Get(memPoolName)
						memPoolClientResponse := rp.updateApplicationLogic(memBlock)
						if rp.debugOn {
							common.Debug("Committed mem block "+memPoolName, 1, rp.debugLevel, rp.debugOn)
						}
						rp.sendMemPoolClientResponse(memPoolClientResponse)

						if rp.debugOn {
							common.Debug("Committed mem block "+memPoolName, 1, rp.debugLevel, rp.debugOn)
						}
					}
					rp.paxosConsensus.lastCommittedRounds[j] = lastMemPoolCounter
				}
			}
			if rp.debugOn {
				common.Debug("Committed paxos consensus instance "+"."+strconv.Itoa(int(i))+" with mem pool indexes "+fmt.Sprintf("%v", nextInstanceToCommit.decisions)+" at time "+fmt.Sprintf("%v", time.Now().Sub(rp.paxosConsensus.startTime)), 42, rp.debugLevel, rp.debugOn)
			}
			rp.paxosConsensus.lastCommittedLogIndex = i
			rp.paxosConsensus.lastCommittedTime = time.Now()
		}
	}
}
