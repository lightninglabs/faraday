# Accounting Reports
Faraday produces accounting reports on a node's on chain and off chain activity. 
These reports are formatted using the [Harmony Reporting Standard](https://github.com/picksco/harmony). 
This document provides a description of the entries in these reports. 

## Common Fields
For brevity, the following fields which have the same meaning for each entry will be omitted: 
- Timestamp: The timestamp of the block that the channel open transaction appeared in. 
- Fiat: The value of the amount field in USD. Note that values less than one satoshi will be rounded down to zero. 
- OnChain: Whether the transaction occurred off chain, or on chain.
- Credit: True when an entry increased our balances, false when an entry decreased our balances. 

Note that fee entries reference the entry they are associated with by appending a fee marker (:-1) to the original reference. The fee entry will have a reference formatted as follows: `original reference:-1`. 

## On Chain Reports

### Local Channel Open
Local channel open entry types represent channel opens that were initiated by 
our node. These entries are accompanied by a separate Channel Open Fees entry, 
because the opening party pays on chain fees. 

- Amount: The amount that we added to the channel, excluding on chain fees. 
- TxID: The on chain transaction ID for the channel open. 
- Reference: The unique channel ID assigned to the channel. 
- Note: A note with details of who opened the channel. 

### Channel Open Fees
The fees paid to open a channel that we initiated. 

- Amount: The amount of on chain fees paid.  
- TxID: The on chain transaction ID for the channel open. 
- Reference: TransactionID:-1; note that this is a special marker for fees. 
- Note: A note containing the pubkey of the peer that we opened the channel to. 

### Remote Channel Open
Remote channel open entry types represent channels that were opened by remote
peers. 

- Amount: Zero, our balance is unaffected by remote channel creation, with the exception of a push amount listed below. 
- TxID: The on chain transaction ID for the channel open. 
- Reference: The unique channel ID assigned to the channel. 
- Note: A note containing the pubkey of the peer that opened a channel to us. 

Known Omissions:
- Remote peers may push balance to our node as part of the funding flow. This amount is not currently included in these reports. 