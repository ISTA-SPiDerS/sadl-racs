import os

# no batching sync
arr = 30
viewTimeout = 30000000
batchTi = 5000
batchSi = 1
networkBatchTim = 3
asyncSimTim = 0

for cons in ["paxos", "async"]:
    for m in [1]:
        os.system("/bin/bash integration-test/safety_test.sh " + str(arr) + " " + str(
            viewTimeout) + " " + str(batchTi) + " " + str(batchSi) + " " + str(
            networkBatchTim) + " " + str(asyncSimTim) + " " + str(cons))

# batching sync
arr = 1000
viewTimeout = 30000000
batchTi = 5000
batchSi = 50
networkBatchTim = 3
asyncSimTim = 0

for cons in ["paxos", "async"]:
    for m in [1]:
        os.system("/bin/bash integration-test/safety_test.sh " + str(arr) + " " + str(
            viewTimeout) + " " + str(batchTi) + " " + str(batchSi) + " " + str(
            networkBatchTim) + " " + str(asyncSimTim) + " " + str(cons))

# async with batching

arr = 10000
viewTimeout = 100000
batchTi = 5000
batchSi = 50
networkBatchTim = 3
asyncSimTim = 120

for cons in ["paxos", "async"]:
    for m in [1]:
        os.system("/bin/bash integration-test/safety_test.sh " + str(arr) + " " + str(
            viewTimeout) + " " + str(batchTi) + " " + str(batchSi) + " " + str(
            networkBatchTim) + " " + str(asyncSimTim) + " " + str(cons))
