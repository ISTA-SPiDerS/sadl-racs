# SADL-RACS

`SADL-RACS` is a robust, high-throughput consensus protocol
for cloud environments. 
It combines a randomized consensus algorithm (`RACS`) 
with an efficient command dissemination layer (`SADL`),
achieving strong consistency under adverse network conditions and excellent
throughput. 
This repository contains the Go implementation of `RACS-SADL`.

## Getting Started

Install `Go 1.19` and `protoc`. 

To build the project, run `build.sh`.

To run locally `/bin/bash integration-test/safety_test.sh 100 300000000 5000 100 5`

## Repository Structure

The repository contains the following key folders: 

- `client/` implements the client software.

- `replica/` contain the `RACS` and `SADL` logic.

- `proto/` holds the protobuf definitions.

- `integration-test/` contains the sample execution with 5 replicas.

## Protocol Overview

`RACS` uses a fast-path Raft-style leader-based synchronous mode
and switches to a randomized fallback mode when network delays or failures
are detected—avoiding traditional view changes. 

`SADL` decouples command propagation from consensus by asynchronously
replicating batches of commands and embedding only compact references in consensus blocks.
This design eliminates the leader’s bandwidth bottleneck and enables up to 500k commands/sec throughput in WAN settings.


## Paper Information

**Title**: RACS-SADL: Robust and Understandable Randomized Consensus in the Cloud

**Authors**: Pasindu Tennage, Antoine Desjardins, Lefteris Kokoris-Kogias  

**Conference**: The IEEE International Conference on Cloud Computing (CLOUD 2025)  

**Location**: Helsinki, Finland

## Citation

If you use this software or build on the protocol, please cite the paper as follows:

`@inproceedings{tennage2025racsadl,  
title = {RACS-SADL: Robust and Understandable Randomized Consensus in the Cloud},  
author = {Tennage, Pasindu and Desjardins, Antoine and Kokoris-Kogias, Lefteris },  
booktitle = {Proceedings of the IEEE International Conference on Cloud Computing (CLOUD)},  
year = {2025},  
address = {Helsinki, Finland}  
`}

## Contact

For questions, collaborations, or bug reports, please open an issue or contact `pasindu.tennage@gmail.com`.

## License

This project is licensed under the `BSD 3-Clause License`.
