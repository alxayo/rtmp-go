// Package srt implements the Secure Reliable Transport (SRT) protocol.
//
// SRT is a UDP-based protocol designed for low-latency, reliable video
// transport across unpredictable networks (like the public internet).
// Unlike TCP, SRT uses selective retransmission and forward error
// correction to recover lost packets without the head-of-line blocking
// that makes TCP unsuitable for live video.
//
// This package tree is organized into sub-packages:
//
//   - packet:    Binary wire format for SRT data and control packets
//   - handshake: SRT connection setup state machine (caller/listener)
//   - conn:      Connection lifecycle and read/write loops
//   - crypto:    AES-CTR encryption for media payload protection
//   - circular:  Circular buffer for packet reordering and loss detection
//
// The SRT protocol specification is maintained by the SRT Alliance:
// https://datatracker.ietf.org/doc/html/draft-sharabayko-srt
package srt
