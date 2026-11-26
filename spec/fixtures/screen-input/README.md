# Pixel scroll records

The live screen's pixel scroll record: 16 bytes the phone sends
inside the RFB stream as client message type 200, which the node takes out and hands to the Mac's Persona Input helper.

`records.json` holds records that decode (`valid`: their hex and what they say) and records that are refused
(`invalid`: their hex and which check refuses them: `length`, `type`, `version`, `kind`, `phase` or `momentum`).
Checked by the node (Go).
