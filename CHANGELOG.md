# Changelog

## Unreleased

- Stop speech queues and cancel active system TTS when the bridge disconnects or the node exits. Thanks @SebTardif! (#7)
- Forward only final speech transcripts to quick actions, voice events, and agent requests. Thanks @SebTardif! (#12)
- Treat leading dashes in spoken text as words instead of espeak options. Thanks @SebTardif! (#11)
- Cancel bridge dialing, pairing, and hello waits on SIGINT/SIGTERM. Thanks @SebTardif! (#6)
- Interrupt bridge reconnect backoff promptly on SIGINT/SIGTERM. Thanks @SebTardif! (#5)
- Drain Brabble output before reaping the process and keep stderr flowing with disabled logging or oversized diagnostics, preventing lost transcripts and stalled recognition.
