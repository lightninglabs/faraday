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

### Channel Close 
Channel close entries represent the on chain close of a channel. 

- Amount: The amount that was paid out to us immediately on channel close. 
- TxID: The on chain transaction ID for the channel close. 
- Reference: The channel close transaction ID.
- Note: A note indicating the type of channel close, and who initiated it. 

Known Omissions: 
- If our balance is encumbered behind a timelock, or in an unresolved htlc, it will not be paid out as part of this transaction and must be resolved by follow up on chain transactions. 
- The fees paid to close channels that we initiated are not currently recorded, this is because balances are taken from the funding output rather than being supplied by the wallet.

### Receipt
A receipt is an on chain transaction which paid to our wallet which was not related to the opening/closing of channels.

- Amount: The amount that was paid to an address controlled by our wallet.
- TxID: The on chain transaction ID.
- Reference: The on chain transaction ID.
- Note: An optional label set on transaction publish (see [lnd transaction labels](https://github.com/lightningnetwork/lnd/blob/master/lnrpc/walletrpc/walletkit.proto#L136)). 

Known Omissions:
- This entry type will include on chain resolutions for channel closes that sweep balances back to our node.

### Payment
A payment is an on chain transaction which was paid from our wallet and was not related to the opening/closing of channels. 
- Amount: The amount that was paid from an address controlled by our wallet.
- TxID: The on chain transaction ID.
- Reference: The on chain transaction ID.
- Note: An optional label set on transaction publish (see [lnd transaction labels](https://github.com/lightningnetwork/lnd/blob/master/lnrpc/walletrpc/walletkit.proto#L136)). 

Known Omissions:
- This entry type will include on chain resolutions for channel closes that require on chain resolutions that spend from our balance. 

### Fee
A fee entry represents the on chain fees we paid for a transaction. 

- Amount: The amount that was paid in fees from our wallet. 
- TxID: The on chain transaction ID.
- Reference: TransactionID:-1. 
- Note: Note set for fees. 

## Off Chain Reports

### Receipt
Receipts off chain represent invoices that are paid via the Lightning Network.

- Amount: The amount that we were paid, note that this may be greater than the original invoice value.
- TxID: the payment hash of the invoice
- Reference: the preimage of the invoice
- Note: Optionally set if the invoice had a memo attached, was overpaid, or was a keysend.

### Circular Receipt
Circular receipts record instances where we have paid one of our own invoices. 

- Amount: The amount that we were paid, note that this may be greater than the original invoice value.
- TxID: the payment hash of the invoice
- Reference: the preimage of the invoice
- Note: Optionally set if the invoice had a memo attached, was overpaid, or was a keysend.
