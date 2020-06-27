# Channel Reports
Channel reports provide details summaries of channels that we have closed on chain, including fees and on chain resolutions required to fully finalize a channel. Note that channels that are still pending close will not be considered for channel reports; you must wait until the channel is fully resolved on chain. Since these channels are opened, closed and resolved on chain, all units will be expressed in satoshis. 

## Common Fields
- Channel Point: The funding txid: output index of of the output which created the channel. 
- Channel Initiator: True if our node opened the channel. 
- Close Type: The type of channel close - cooperative, local force, remote force, breach or justice.
- Open Fee: The fees we paid to open the channel in satoshis, note that this amount will be 0 if we did not open the channel. 
- Close Fee: The fees we paid to close the channel in satoshis, not that this amount will be 0 if we did not open the channel.

### Cooperative Close
A cooperative close occurs when one party decides that they want to close the channel, and the other is online to cooperatively sign a close transaction. When this kind of close occurs, there are no on chain resolutions because the parties agree to wait for all htlcs to clear, and sign a close transaction which pays out each party without encumbering their funds behind a time lock. 

Since this close type has no on chain resolutions, there are no fields in the report aside from the common fields listed above. 

Known Omissions:
- The current implementation does not support generation of reports for channels that were created with batched funding transactions. 