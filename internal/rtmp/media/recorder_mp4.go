package media

// MP4 Recorder — Streaming MP4 writer for RTMP media
// ===================================================
//
// Converts RTMP media messages into playable ISO BMFF (MP4) files. Handles both
// legacy RTMP and Enhanced RTMP payload formats, stripping envelope headers before
// writing raw codec data to the mdat box.
//
// Architecture:
//   - WriteMessage() is called on every audio/video RTMP message
//   - Sequence headers (codec config) are captured for moov box, NOT written to mdat
//   - Coded frames have RTMP envelope stripped; only raw codec data goes to mdat
//   - On Close(), mdat size is patched and moov box is appended with sample tables
//
// Connection to other files:
//   - recorder.go: NewRecorder() routes H.265+ here, H.264 to FLVRecorder
//   - video.go / audio.go: ParseVideoMessage/ParseAudioMessage parse same payloads
//   - codec package (internal/codec/): Builds RTMP payloads that we decode here
//   - command_integration.go / srt_accept.go: ensureRecorder() creates this recorder
//   - media_dispatch.go: Calls WriteMessage() on every media frame

import (
"encoding/binary"
"fmt"
"io"
"log/slog"
"os"
"sync"

"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// MP4Recorder writes RTMP media to MP4 format, streaming frames directly to disk.
//
// File layout: [ftyp box][mdat box (streamed)][moov box (written on Close)]
//
// RTMP payloads contain envelope headers (frame type, codec ID, FourCC, etc.)
// that do NOT belong in MP4 mdat. This recorder strips those envelopes and
// writes only the raw codec data (length-prefixed NALUs for video, raw AAC
// frames for audio). Sequence headers are captured for the moov/stsd boxes.
//
// Memory usage is O(number_of_samples) for sample metadata only.
type MP4Recorder struct {
mu           sync.Mutex
file         *os.File
logger       *slog.Logger
disabled     bool
videoSamples []mp4VideoSample // per-video-frame metadata (separate from audio)
audioSamples []mp4AudioSample // per-audio-frame metadata (separate from video)
videoConfig  []byte           // HEVCDecoderConfigurationRecord or AVCDecoderConfigurationRecord
audioConfig  []byte           // AudioSpecificConfig (from sequence header)
videoCodec   string           // detected video codec: "H265", "H264", "AV1", "VP9", "VP8", "VVC"
audioCodec   string           // detected audio codec: "AAC", "Opus", "FLAC", "AC3", "EAC3", "MP3"
mdatStart    int64            // file offset where mdat box begins
mdatDataSize int64            // total bytes written to mdat so far
}

// mp4VideoSample stores per-frame metadata for the video track.
// Maps to MP4 boxes: size→stsz, timestamp→stts, ctsOffset→ctts, isKey→stss.
type mp4VideoSample struct {
offset    int64  // file offset of sample data within mdat
size      int32  // byte size of raw codec data
timestamp uint32 // DTS in milliseconds
ctsOffset int32  // composition time offset (PTS = DTS + CTS)
isKey     bool   // true for keyframes → stss box
}

// mp4AudioSample stores per-frame metadata for the audio track.
type mp4AudioSample struct {
offset    int64  // file offset of sample data within mdat
size      int32  // byte size of raw AAC frame
timestamp uint32 // DTS in milliseconds
}

const (
ftypBoxSize = 32 // fixed ftyp box size
mdatHdrSize = 8  // mdat box header (size + "mdat")
)

// NewMP4Recorder creates an MP4 recorder writing to the given path.
// Writes ftyp box and mdat header placeholder immediately.
func NewMP4Recorder(path string, logger *slog.Logger) (MediaWriter, error) {
if logger == nil {
logger = slog.Default()
}
f, err := os.Create(path)
if err != nil {
return nil, fmt.Errorf("mp4_recorder.create: %w", err)
}

r := &MP4Recorder{
file:         f,
logger:       logger,
videoSamples: make([]mp4VideoSample, 0, 512),
audioSamples: make([]mp4AudioSample, 0, 512),
mdatStart:    ftypBoxSize,
}

if err := r.writeFtypBox(); err != nil {
f.Close()
return nil, fmt.Errorf("mp4_recorder.ftyp: %w", err)
}

// Write mdat header with placeholder size (patched on Close)
var mdatHdr [mdatHdrSize]byte
copy(mdatHdr[4:], []byte("mdat"))
if _, err := f.Write(mdatHdr[:]); err != nil {
f.Close()
return nil, fmt.Errorf("mp4_recorder.mdat_header: %w", err)
}

return r, nil
}

// WriteMessage processes an RTMP audio (TypeID 8) or video (TypeID 9) message.
// It strips the RTMP envelope header and writes only raw codec data to mdat.
// Sequence headers are captured for the moov box and NOT written to mdat.
func (r *MP4Recorder) WriteMessage(msg *chunk.Message) {
if msg == nil || (msg.TypeID != 8 && msg.TypeID != 9) {
return
}
r.mu.Lock()
defer r.mu.Unlock()

if r.disabled || r.file == nil {
return
}

if msg.TypeID == 9 {
r.handleVideoMessage(msg)
} else {
r.handleAudioMessage(msg)
}
}

// handleVideoMessage strips the RTMP video envelope and writes raw NALUs to mdat.
//
// RTMP video payload formats:
//
// Enhanced RTMP (IsExHeader=1, byte[0] bit 7 set):
//   [ExHeader(1B)][FourCC(4B)][CTS?(3B)][NALUs...]
//   - SequenceStart (pktType=0): config record after FourCC, no CTS
//   - CodedFrames (pktType=1): 3-byte CTS after FourCC, then NALUs
//   - CodedFramesX (pktType=3): no CTS, NALUs directly after FourCC
//
// Legacy (IsExHeader=0):
//   [FrameType+CodecID(1B)][AVCPacketType(1B)][CTS(3B)][data...]
//   - SequenceHeader (pt=0): config record at offset 5
//   - NALU (pt=1): NALUs at offset 5
func (r *MP4Recorder) handleVideoMessage(msg *chunk.Message) {
data := msg.Payload
if len(data) < 2 {
return
}

b0 := data[0]
isEnhanced := (b0 >> 7) & 1

if isEnhanced == 1 {
r.handleEnhancedVideo(msg, data)
} else {
r.handleLegacyVideo(msg, data)
}
}

// handleEnhancedVideo processes Enhanced RTMP video messages (E-RTMP v2).
func (r *MP4Recorder) handleEnhancedVideo(msg *chunk.Message, data []byte) {
if len(data) < 5 {
return
}

b0 := data[0]
frameTypeID := (b0 >> 4) & 0x07
pktType := b0 & 0x0F
fourCC := string(data[1:5])

// Detect codec from FourCC
switch fourCC {
case "hvc1":
r.videoCodec = "H265"
case "avc1":
r.videoCodec = "H264"
case "av01":
r.videoCodec = "AV1"
case "vp09":
r.videoCodec = "VP9"
case "vp08":
r.videoCodec = "VP8"
}

isKey := frameTypeID == 1

switch pktType {
case 0: // SequenceStart — codec configuration record
// Store the raw config record (everything after FourCC) for moov/stsd
r.videoConfig = make([]byte, len(data[5:]))
copy(r.videoConfig, data[5:])

case 1: // CodedFrames — has 3-byte CTS after FourCC
if len(data) < 8 {
return
}
// Extract signed 24-bit CTS offset
cts := int32(data[5])<<16 | int32(data[6])<<8 | int32(data[7])
if cts&0x800000 != 0 {
cts |= ^0xFFFFFF // sign extend
}
naluData := data[8:]
r.writeVideoSample(naluData, msg.Timestamp, cts, isKey)

case 3: // CodedFramesX — no CTS (DTS==PTS)
naluData := data[5:]
r.writeVideoSample(naluData, msg.Timestamp, 0, isKey)

case 2: // SequenceEnd — ignore
case 4: // Metadata — ignore
}
}

// handleLegacyVideo processes traditional RTMP video messages (4-bit CodecID).
func (r *MP4Recorder) handleLegacyVideo(msg *chunk.Message, data []byte) {
if len(data) < 5 {
return
}

b0 := data[0]
frameTypeID := (b0 >> 4) & 0x0F
codecID := b0 & 0x0F
pktType := data[1]

isKey := frameTypeID == 1

switch codecID {
case 7: // AVC (H.264)
r.videoCodec = "H264"
case 12: // HEVC (non-standard legacy extension)
r.videoCodec = "H265"
default:
return // unsupported codec
}

// Extract signed 24-bit CTS from bytes 2-4
cts := int32(data[2])<<16 | int32(data[3])<<8 | int32(data[4])
if cts&0x800000 != 0 {
cts |= ^0xFFFFFF // sign extend
}

switch pktType {
case 0: // Sequence header — config record at offset 5
r.videoConfig = make([]byte, len(data[5:]))
copy(r.videoConfig, data[5:])
case 1: // NALU — raw NALUs at offset 5
r.writeVideoSample(data[5:], msg.Timestamp, cts, isKey)
}
}

// writeVideoSample writes raw video codec data to mdat and records metadata.
func (r *MP4Recorder) writeVideoSample(naluData []byte, timestamp uint32, cts int32, isKey bool) {
if len(naluData) == 0 {
return
}

offset := r.mdatStart + mdatHdrSize + r.mdatDataSize

if _, err := r.file.Write(naluData); err != nil {
r.logger.Error("mp4_recorder video write failed", "err", err)
r.disabled = true
return
}

r.videoSamples = append(r.videoSamples, mp4VideoSample{
offset:    offset,
size:      int32(len(naluData)),
timestamp: timestamp,
ctsOffset: cts,
isKey:     isKey,
})
r.mdatDataSize += int64(len(naluData))
}

// handleAudioMessage strips the RTMP audio envelope and writes raw AAC to mdat.
//
// Legacy AAC: [0xAF][PacketType(1B)][data...]
//   - PacketType 0: AudioSpecificConfig (captured, not written to mdat)
//   - PacketType 1: Raw AAC frame (written to mdat)
//
// Enhanced RTMP audio (SoundFormat=9):
//   [ExHeader(1B)][FourCC(4B)][data...]
//   - PacketType 0: SequenceStart (config)
//   - PacketType 1: CodedFrames (raw audio)
func (r *MP4Recorder) handleAudioMessage(msg *chunk.Message) {
data := msg.Payload
if len(data) < 2 {
return
}

soundFormat := (data[0] >> 4) & 0x0F

if soundFormat == 9 {
// Enhanced RTMP audio
r.handleEnhancedAudio(msg, data)
} else if soundFormat == 10 {
// Legacy AAC
r.handleLegacyAAC(msg, data)
}
// Other audio codecs not using enhanced RTMP are not supported in MP4 recorder
}

// handleEnhancedAudio processes Enhanced RTMP audio messages.
// Enhanced audio payload: [ExHeader(1B)][FourCC(4B)][data...]
//   - FourCC identifies the audio codec (mp4a=AAC, Opus, fLaC, ac-3, ec-3, .mp3)
//   - pktType 0: SequenceStart — codec configuration record after FourCC
//   - pktType 1: CodedFrames — raw audio data after FourCC
func (r *MP4Recorder) handleEnhancedAudio(msg *chunk.Message, data []byte) {
if len(data) < 5 {
return
}

pktType := data[0] & 0x0F
fourCC := string(data[1:5])

// Detect audio codec from the FourCC identifier in the enhanced audio header.
// Each FourCC maps to a specific codec that determines which MP4 sample entry
// box we'll generate later in buildAudioSampleTable().
switch fourCC {
case "mp4a":
r.audioCodec = "AAC"
case "Opus":
r.audioCodec = "Opus"
case "fLaC":
r.audioCodec = "FLAC"
case "ac-3":
r.audioCodec = "AC3"
case "ec-3":
r.audioCodec = "EAC3"
case ".mp3":
r.audioCodec = "MP3"
}

switch pktType {
case 0: // SequenceStart — codec configuration record after FourCC
r.audioConfig = make([]byte, len(data[5:]))
copy(r.audioConfig, data[5:])
case 1: // CodedFrames — raw audio after FourCC
r.writeAudioSample(data[5:], msg.Timestamp)
}
}

// handleLegacyAAC processes traditional RTMP AAC audio messages.
// Legacy AAC is always codec "AAC" — set audioCodec so buildAudioSampleTable()
// uses the correct mp4a/esds sample entry.
func (r *MP4Recorder) handleLegacyAAC(msg *chunk.Message, data []byte) {
r.audioCodec = "AAC"
pktType := data[1]

switch pktType {
case 0: // Sequence header — AudioSpecificConfig at offset 2
r.audioConfig = make([]byte, len(data[2:]))
copy(r.audioConfig, data[2:])
case 1: // Raw AAC frame at offset 2
r.writeAudioSample(data[2:], msg.Timestamp)
}
}

// writeAudioSample writes raw AAC frame data to mdat and records metadata.
func (r *MP4Recorder) writeAudioSample(rawAudio []byte, timestamp uint32) {
if len(rawAudio) == 0 {
return
}

offset := r.mdatStart + mdatHdrSize + r.mdatDataSize

if _, err := r.file.Write(rawAudio); err != nil {
r.logger.Error("mp4_recorder audio write failed", "err", err)
r.disabled = true
return
}

r.audioSamples = append(r.audioSamples, mp4AudioSample{
offset:    offset,
size:      int32(len(rawAudio)),
timestamp: timestamp,
})
r.mdatDataSize += int64(len(rawAudio))
}

// Disabled returns true if the recorder encountered a fatal write error.
func (r *MP4Recorder) Disabled() bool {
r.mu.Lock()
defer r.mu.Unlock()
return r.disabled
}

// Close patches the mdat size header and appends the moov box with all track
// metadata and sample tables. The file is always closed, even on error.
func (r *MP4Recorder) Close() error {
r.mu.Lock()
defer r.mu.Unlock()

if r.file == nil {
return nil
}
defer func() {
r.file.Close()
r.file = nil
}()

totalSamples := len(r.videoSamples) + len(r.audioSamples)
if r.disabled || totalSamples == 0 {
return nil
}

// Patch mdat box size
mdatBoxSize := uint32(mdatHdrSize + r.mdatDataSize)
var sizeBuf [4]byte
binary.BigEndian.PutUint32(sizeBuf[:], mdatBoxSize)
if _, err := r.file.WriteAt(sizeBuf[:], r.mdatStart); err != nil {
return fmt.Errorf("mp4_recorder.patch_mdat: %w", err)
}

// Append moov box
if err := r.writeMoovBox(r.file); err != nil {
return fmt.Errorf("mp4_recorder.moov: %w", err)
}

return nil
}

// lastTimestamp returns the highest timestamp across all samples.
func (r *MP4Recorder) lastTimestamp() uint32 {
var last uint32
if n := len(r.videoSamples); n > 0 {
if ts := r.videoSamples[n-1].timestamp; ts > last {
last = ts
}
}
if n := len(r.audioSamples); n > 0 {
if ts := r.audioSamples[n-1].timestamp; ts > last {
last = ts
}
}
return last
}

// =============================================================================
// MP4 box generation (moov and its children)
// =============================================================================

func (r *MP4Recorder) writeFtypBox() error {
// ftyp: isom brand, compatible with isom, iso2, avc1, hev1
ftypData := []byte{
0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p',
'i', 's', 'o', 'm', 0x00, 0x00, 0x00, 0x00,
'i', 's', 'o', 'm', 'i', 's', 'o', '2',
'a', 'v', 'c', '1', 'h', 'e', 'v', '1',
}
_, err := r.file.Write(ftypData)
return err
}

func (r *MP4Recorder) writeMoovBox(w io.Writer) error {
moovBuf := newMP4BoxBuilder()

duration := r.lastTimestamp()

// mvhd — Movie Header Box (version 0)
mvhdBuf := newMP4BoxBuilder()
mvhdBuf.writeU32(0)          // version + flags
mvhdBuf.writeU32(0)          // creation_time
mvhdBuf.writeU32(0)          // modification_time
mvhdBuf.writeU32(1000)       // timescale (1ms units)
mvhdBuf.writeU32(duration)   // duration
mvhdBuf.writeU32(0x00010000) // rate (1.0 fixed-point)
mvhdBuf.writeU16(0x0100)     // volume (1.0 fixed-point)
mvhdBuf.writeBytes(make([]byte, 10)) // reserved
mvhdBuf.writeBytes(identityMatrix())
// pre_defined (6 x uint32)
mvhdBuf.writeBytes(make([]byte, 24))
nextTrackID := uint32(1)
if len(r.videoSamples) > 0 {
nextTrackID++
}
if len(r.audioSamples) > 0 {
nextTrackID++
}
mvhdBuf.writeU32(nextTrackID)
moovBuf.writeBox("mvhd", mvhdBuf.Bytes())

if len(r.videoSamples) > 0 {
moovBuf.writeBox("trak", r.buildVideoTrack(duration).Bytes())
}
if len(r.audioSamples) > 0 {
moovBuf.writeBox("trak", r.buildAudioTrack(duration).Bytes())
}

return r.writeBoxHeader(w, "moov", moovBuf.Bytes())
}

func (r *MP4Recorder) buildVideoTrack(duration uint32) *mp4BoxBuilder {
trakBuf := newMP4BoxBuilder()

// tkhd — Track Header (version 0, flags=0x0F: enabled+in_movie+in_preview+size_is_aspect_ratio)
tkhdBuf := newMP4BoxBuilder()
tkhdBuf.writeU32(0x0000000F) // version 0 + flags
tkhdBuf.writeU32(0)          // creation_time
tkhdBuf.writeU32(0)          // modification_time
tkhdBuf.writeU32(1)          // track_ID
tkhdBuf.writeU32(0)          // reserved
tkhdBuf.writeU32(duration)   // duration
tkhdBuf.writeBytes(make([]byte, 8)) // reserved
tkhdBuf.writeU16(0)          // layer
tkhdBuf.writeU16(0)          // alternate_group
tkhdBuf.writeU16(0)          // volume (0 for video)
tkhdBuf.writeU16(0)          // reserved
tkhdBuf.writeBytes(identityMatrix())
tkhdBuf.writeU32(0x04b00000) // width 1200.0 (fixed-point 16.16)
tkhdBuf.writeU32(0x02d00000) // height 720.0 (fixed-point 16.16)
trakBuf.writeBox("tkhd", tkhdBuf.Bytes())

// mdia — Media Box
mediaBuf := newMP4BoxBuilder()

// mdhd — Media Header (timescale = 1000 for ms)
mdhdBuf := newMP4BoxBuilder()
mdhdBuf.writeU32(0)        // version + flags
mdhdBuf.writeU32(0)        // creation_time
mdhdBuf.writeU32(0)        // modification_time
mdhdBuf.writeU32(1000)     // timescale
mdhdBuf.writeU32(duration) // duration
mdhdBuf.writeU16(0x55C4)   // language (undetermined)
mdhdBuf.writeU16(0)        // pre_defined
mediaBuf.writeBox("mdhd", mdhdBuf.Bytes())

// hdlr — Handler Reference
hdlrBuf := newMP4BoxBuilder()
hdlrBuf.writeU32(0)             // version + flags
hdlrBuf.writeU32(0)             // pre_defined
hdlrBuf.writeBytes([]byte("vide")) // handler_type
hdlrBuf.writeBytes(make([]byte, 12)) // reserved
hdlrBuf.writeBytes([]byte("VideoHandler\x00"))
mediaBuf.writeBox("hdlr", hdlrBuf.Bytes())

// minf — Media Information
minfBuf := newMP4BoxBuilder()

// vmhd — Video Media Header
vmhdBuf := newMP4BoxBuilder()
vmhdBuf.writeU32(0x00000001) // version 0 + flag 1 (required for vmhd)
vmhdBuf.writeU16(0)          // graphicsmode
vmhdBuf.writeU16(0)          // opcolor[0]
vmhdBuf.writeU16(0)          // opcolor[1]
vmhdBuf.writeU16(0)          // opcolor[2]
minfBuf.writeBox("vmhd", vmhdBuf.Bytes())

minfBuf.writeBox("dinf", buildDinf().Bytes())

// stbl — Sample Table
stblBuf := r.buildVideoSampleTable()
minfBuf.writeBox("stbl", stblBuf.Bytes())

mediaBuf.writeBox("minf", minfBuf.Bytes())
trakBuf.writeBox("mdia", mediaBuf.Bytes())

return trakBuf
}

func (r *MP4Recorder) buildAudioTrack(duration uint32) *mp4BoxBuilder {
trakBuf := newMP4BoxBuilder()

trackID := uint32(2)
if len(r.videoSamples) == 0 {
trackID = 1
}

// tkhd
tkhdBuf := newMP4BoxBuilder()
tkhdBuf.writeU32(0x0000000F) // version 0 + flags
tkhdBuf.writeU32(0)          // creation_time
tkhdBuf.writeU32(0)          // modification_time
tkhdBuf.writeU32(trackID)    // track_ID
tkhdBuf.writeU32(0)          // reserved
tkhdBuf.writeU32(duration)   // duration
tkhdBuf.writeBytes(make([]byte, 8)) // reserved
tkhdBuf.writeU16(0)          // layer
tkhdBuf.writeU16(0)          // alternate_group
tkhdBuf.writeU16(0x0100)     // volume (1.0 for audio)
tkhdBuf.writeU16(0)          // reserved
tkhdBuf.writeBytes(identityMatrix())
tkhdBuf.writeU32(0) // width (0 for audio)
tkhdBuf.writeU32(0) // height (0 for audio)
trakBuf.writeBox("tkhd", tkhdBuf.Bytes())

// mdia
mediaBuf := newMP4BoxBuilder()

// Parse AudioSpecificConfig to get sample rate
// For AAC, we parse the 2-byte AudioSpecificConfig from the sequence header.
// For other codecs, we use codec-specific defaults or parse their config data.
sampleRate := uint32(44100)
channels := uint16(2)

switch r.audioCodec {
case "Opus":
// Opus in MP4 always uses 48kHz timescale per RFC 7845 / ISO 14496-12
sampleRate = 48000
channels = 2 // default stereo; overridden from OpusHead if available
if len(r.audioConfig) >= 19 {
// OpusHead: offset 9 = channel count
channels = uint16(r.audioConfig[9])
if channels == 0 {
	channels = 2
}
}
case "FLAC":
// Parse FLAC STREAMINFO block to get sample rate and channels.
// STREAMINFO layout (34 bytes): bytes 10-13 contain sample rate (20 bits),
// bits after that contain channel count - 1 (3 bits).
if len(r.audioConfig) >= 18 {
sr := uint32(r.audioConfig[10])<<12 | uint32(r.audioConfig[11])<<4 | uint32(r.audioConfig[12])>>4
if sr > 0 {
	sampleRate = sr
}
ch := uint16((r.audioConfig[12]>>1)&0x07) + 1
if ch > 0 {
	channels = ch
}
}
case "AC3", "EAC3":
// AC-3 and E-AC-3 are almost always 48kHz with 5.1 surround
sampleRate = 48000
channels = 6
case "MP3":
// Common MP3 defaults: 44100 Hz stereo
sampleRate = 44100
channels = 2
default:
// AAC or unknown — parse AudioSpecificConfig (2+ bytes)
if len(r.audioConfig) >= 2 {
// AudioSpecificConfig: 5-bit object type, 4-bit freq index, 4-bit channels
freqIdx := (r.audioConfig[0]&0x07)<<1 | (r.audioConfig[1] >> 7)
ch := (r.audioConfig[1] >> 3) & 0x0F
aacFreqs := []uint32{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
if int(freqIdx) < len(aacFreqs) {
	sampleRate = aacFreqs[freqIdx]
}
if ch > 0 {
	channels = uint16(ch)
}
}
}

// mdhd — use audio sample rate as timescale for precision
mdhdBuf := newMP4BoxBuilder()
mdhdBuf.writeU32(0)          // version + flags
mdhdBuf.writeU32(0)          // creation_time
mdhdBuf.writeU32(0)          // modification_time
mdhdBuf.writeU32(sampleRate) // timescale = sample rate
// Duration in audio timescale units
audioDuration := uint32(uint64(duration) * uint64(sampleRate) / 1000)
mdhdBuf.writeU32(audioDuration)
mdhdBuf.writeU16(0x55C4) // language (undetermined)
mdhdBuf.writeU16(0)      // pre_defined
mediaBuf.writeBox("mdhd", mdhdBuf.Bytes())

// hdlr
hdlrBuf := newMP4BoxBuilder()
hdlrBuf.writeU32(0)
hdlrBuf.writeU32(0)
hdlrBuf.writeBytes([]byte("soun"))
hdlrBuf.writeBytes(make([]byte, 12))
hdlrBuf.writeBytes([]byte("SoundHandler\x00"))
mediaBuf.writeBox("hdlr", hdlrBuf.Bytes())

// minf
minfBuf := newMP4BoxBuilder()

// smhd
smhdBuf := newMP4BoxBuilder()
smhdBuf.writeU32(0) // version + flags
smhdBuf.writeU16(0) // balance
smhdBuf.writeU16(0) // reserved
minfBuf.writeBox("smhd", smhdBuf.Bytes())

minfBuf.writeBox("dinf", buildDinf().Bytes())

stblBuf := r.buildAudioSampleTable(sampleRate, channels)
minfBuf.writeBox("stbl", stblBuf.Bytes())

mediaBuf.writeBox("minf", minfBuf.Bytes())
trakBuf.writeBox("mdia", mediaBuf.Bytes())

return trakBuf
}

// buildDinf builds the Data Information box (shared by video and audio tracks).
func buildDinf() *mp4BoxBuilder {
dinfBuf := newMP4BoxBuilder()
drefBuf := newMP4BoxBuilder()
drefBuf.writeU32(0) // version + flags
drefBuf.writeU32(1) // entry_count
urlBuf := newMP4BoxBuilder()
urlBuf.writeU32(1) // flags = 1 (self-contained)
drefBuf.writeBox("url ", urlBuf.Bytes())
dinfBuf.writeBox("dref", drefBuf.Bytes())
return dinfBuf
}

// buildVideoSampleTable builds the stbl box for the video track.
func (r *MP4Recorder) buildVideoSampleTable() *mp4BoxBuilder {
stblBuf := newMP4BoxBuilder()
samples := r.videoSamples

// --- stsd: Sample Description ---
stsdBuf := newMP4BoxBuilder()
stsdBuf.writeU32(0) // version + flags
stsdBuf.writeU32(1) // entry_count

sampleEntry := newMP4BoxBuilder()
// Reserved (6 bytes) + data_reference_index (2 bytes)
sampleEntry.writeBytes(make([]byte, 6))
sampleEntry.writeU16(1)
// Visual sample entry fields
sampleEntry.writeU16(0) // pre_defined
sampleEntry.writeU16(0) // reserved
sampleEntry.writeBytes(make([]byte, 12)) // pre_defined
sampleEntry.writeU16(1280)       // width
sampleEntry.writeU16(720)        // height
sampleEntry.writeU32(0x00480000) // horizresolution (72 dpi)
sampleEntry.writeU32(0x00480000) // vertresolution (72 dpi)
sampleEntry.writeU32(0)          // reserved
sampleEntry.writeU16(1)          // frame_count
sampleEntry.writeBytes(make([]byte, 32)) // compressorname
sampleEntry.writeU16(0x0018) // depth (24-bit)
sampleEntry.writeU16(0xFFFF) // pre_defined

// Determine codec-specific MP4 box types.
// Each codec has its own configuration box (e.g., avcC for H.264) and
// sample entry box (e.g., avc1 for H.264) in the MP4 stsd atom.
configBoxType := "hvcC" // Default for H.265/HEVC
entryBoxType := "hvc1"
switch r.videoCodec {
case "H264":
configBoxType = "avcC"
entryBoxType = "avc1"
case "AV1":
configBoxType = "av1C"
entryBoxType = "av01"
case "VP9":
configBoxType = "vpcC"
entryBoxType = "vp09"
case "VP8":
configBoxType = "vpcC"
entryBoxType = "vp08"
case "VVC":
configBoxType = "vvcC"
entryBoxType = "vvc1"
}
if len(r.videoConfig) > 0 {
sampleEntry.writeBox(configBoxType, r.videoConfig)
} else {
// Fallback: empty config (file may not be fully playable)
sampleEntry.writeBox(configBoxType, []byte{})
}
stsdBuf.writeBox(entryBoxType, sampleEntry.Bytes())
stblBuf.writeBox("stsd", stsdBuf.Bytes())

// --- stts: Decoding Time to Sample ---
// Use actual per-sample delta from timestamps
sttsEntries := buildSttsEntries(videoTimestamps(samples))
sttsBuf := newMP4BoxBuilder()
sttsBuf.writeU32(0) // version + flags
sttsBuf.writeU32(uint32(len(sttsEntries)))
for _, e := range sttsEntries {
sttsBuf.writeU32(e.count)
sttsBuf.writeU32(e.delta)
}
stblBuf.writeBox("stts", sttsBuf.Bytes())

// --- ctts: Composition Time to Sample (only if any CTS != 0) ---
hasCTS := false
for _, s := range samples {
if s.ctsOffset != 0 {
hasCTS = true
break
}
}
if hasCTS {
cttsBuf := newMP4BoxBuilder()
cttsBuf.writeU32(0x01000000) // version 1 (signed offsets) + flags
// Run-length encode CTS offsets
type cttsEntry struct {
count  uint32
offset int32
}
var cttsEntries []cttsEntry
for _, s := range samples {
if len(cttsEntries) > 0 && cttsEntries[len(cttsEntries)-1].offset == s.ctsOffset {
cttsEntries[len(cttsEntries)-1].count++
} else {
cttsEntries = append(cttsEntries, cttsEntry{1, s.ctsOffset})
}
}
cttsBuf.writeU32(uint32(len(cttsEntries)))
for _, e := range cttsEntries {
cttsBuf.writeU32(e.count)
cttsBuf.writeU32(uint32(e.offset)) // signed as uint32 per spec v1
}
stblBuf.writeBox("ctts", cttsBuf.Bytes())
}

// --- stss: Sync Sample (keyframes) ---
var keyframes []uint32
for i, s := range samples {
if s.isKey {
keyframes = append(keyframes, uint32(i+1)) // 1-based
}
}
if len(keyframes) > 0 && len(keyframes) < len(samples) {
// Only write stss if not all frames are keyframes
sssBuf := newMP4BoxBuilder()
sssBuf.writeU32(0) // version + flags
sssBuf.writeU32(uint32(len(keyframes)))
for _, k := range keyframes {
sssBuf.writeU32(k)
}
stblBuf.writeBox("stss", sssBuf.Bytes())
}

// --- stsz: Sample Size ---
stszBuf := newMP4BoxBuilder()
stszBuf.writeU32(0) // version + flags
stszBuf.writeU32(0) // sample_size = 0 (variable)
stszBuf.writeU32(uint32(len(samples)))
for _, s := range samples {
stszBuf.writeU32(uint32(s.size))
}
stblBuf.writeBox("stsz", stszBuf.Bytes())

// --- stsc: Sample to Chunk (one sample per chunk) ---
stscBuf := newMP4BoxBuilder()
stscBuf.writeU32(0) // version + flags
stscBuf.writeU32(1) // entry_count
stscBuf.writeU32(1) // first_chunk
stscBuf.writeU32(1) // samples_per_chunk
stscBuf.writeU32(1) // sample_description_index
stblBuf.writeBox("stsc", stscBuf.Bytes())

// --- stco: Chunk Offset (one chunk per sample) ---
stcoBuf := newMP4BoxBuilder()
stcoBuf.writeU32(0) // version + flags
stcoBuf.writeU32(uint32(len(samples)))
for _, s := range samples {
stcoBuf.writeU32(uint32(s.offset))
}
stblBuf.writeBox("stco", stcoBuf.Bytes())

return stblBuf
}

// buildAudioSampleTable builds the stbl box for the audio track.
// Generates codec-specific sample description (stsd) entries based on
// r.audioCodec, then the shared timing/size/chunk/offset tables.
func (r *MP4Recorder) buildAudioSampleTable(sampleRate uint32, channels uint16) *mp4BoxBuilder {
stblBuf := newMP4BoxBuilder()
samples := r.audioSamples

// --- stsd: Sample Description ---
// Each audio codec requires a different sample entry box type and
// codec-specific configuration box inside it.
stsdBuf := newMP4BoxBuilder()
stsdBuf.writeU32(0) // version + flags
stsdBuf.writeU32(1) // entry_count

switch r.audioCodec {
case "Opus":
stsdBuf.writeBox("Opus", r.buildOpusSampleEntry(sampleRate, channels).Bytes())
case "FLAC":
stsdBuf.writeBox("fLaC", r.buildFLACSampleEntry(sampleRate, channels).Bytes())
case "AC3":
stsdBuf.writeBox("ac-3", r.buildAC3SampleEntry(sampleRate, channels).Bytes())
case "EAC3":
stsdBuf.writeBox("ec-3", r.buildEAC3SampleEntry(sampleRate, channels).Bytes())
case "MP3":
stsdBuf.writeBox(".mp3", r.buildMP3SampleEntry(sampleRate, channels).Bytes())
default: // AAC or unknown → existing mp4a/esds path
stsdBuf.writeBox("mp4a", r.buildAACSampleEntry(sampleRate, channels).Bytes())
}

stblBuf.writeBox("stsd", stsdBuf.Bytes())

// --- stts ---
// Audio frame duration varies by codec:
//   - AAC: 1024 samples per frame
//   - Opus: 960 samples per frame (20ms at 48kHz)
//   - FLAC/AC3/EAC3/MP3: use timestamp-based deltas
sttsEntries := buildAudioSttsEntries(samples, sampleRate, r.audioCodec)
sttsBuf := newMP4BoxBuilder()
sttsBuf.writeU32(0) // version + flags
sttsBuf.writeU32(uint32(len(sttsEntries)))
for _, e := range sttsEntries {
sttsBuf.writeU32(e.count)
sttsBuf.writeU32(e.delta)
}
stblBuf.writeBox("stts", sttsBuf.Bytes())

// --- stsz ---
stszBuf := newMP4BoxBuilder()
stszBuf.writeU32(0) // version + flags
stszBuf.writeU32(0) // sample_size = 0 (variable)
stszBuf.writeU32(uint32(len(samples)))
for _, s := range samples {
stszBuf.writeU32(uint32(s.size))
}
stblBuf.writeBox("stsz", stszBuf.Bytes())

// --- stsc ---
stscBuf := newMP4BoxBuilder()
stscBuf.writeU32(0) // version + flags
stscBuf.writeU32(1) // entry_count
stscBuf.writeU32(1) // first_chunk
stscBuf.writeU32(1) // samples_per_chunk
stscBuf.writeU32(1) // sample_description_index
stblBuf.writeBox("stsc", stscBuf.Bytes())

// --- stco ---
stcoBuf := newMP4BoxBuilder()
stcoBuf.writeU32(0) // version + flags
stcoBuf.writeU32(uint32(len(samples)))
for _, s := range samples {
stcoBuf.writeU32(uint32(s.offset))
}
stblBuf.writeBox("stco", stcoBuf.Bytes())

return stblBuf
}

// buildEsdsBox creates an MPEG-4 Elementary Stream Descriptor with the actual
// AudioSpecificConfig from the AAC sequence header.
func (r *MP4Recorder) buildEsdsBox() []byte {
asc := r.audioConfig
if len(asc) == 0 {
// Fallback: AAC-LC, 44100 Hz, stereo
asc = []byte{0x12, 0x10}
}

// ES_Descriptor structure:
//   tag=0x03, length, ES_ID, flags,
//   DecoderConfigDescriptor (tag=0x04, length, objectTypeIndication, ...),
//     DecoderSpecificInfo (tag=0x05, length, AudioSpecificConfig),
//   SLConfigDescriptor (tag=0x06, length=1, value=2)

decSpecInfoLen := len(asc)
decConfigLen := 13 + 2 + decSpecInfoLen // 13 fixed + tag+len + ASC
esDescLen := 3 + 2 + decConfigLen + 3   // ES_ID+flags + tag+len + decConfig + SLConfig

buf := newMP4BoxBuilder()
buf.writeU32(0) // version + flags

// ES_Descriptor
buf.writeU8(0x03)              // tag
buf.writeU8(uint8(esDescLen))  // length
buf.writeU16(0)                // ES_ID
buf.writeU8(0)                 // flags

// DecoderConfigDescriptor
buf.writeU8(0x04)                // tag
buf.writeU8(uint8(decConfigLen)) // length
buf.writeU8(0x40)                // objectTypeIndication: Audio ISO/IEC 14496-3
buf.writeU8(0x15)                // streamType=5 (audio) upstream=0 reserved=1
buf.writeU8(0x00)                // bufferSizeDB (3 bytes)
buf.writeU16(0x0000)
buf.writeU32(0)                  // maxBitrate
buf.writeU32(0)                  // avgBitrate

// DecoderSpecificInfo
buf.writeU8(0x05)                   // tag
buf.writeU8(uint8(decSpecInfoLen))  // length
buf.writeBytes(asc)                 // AudioSpecificConfig

// SLConfigDescriptor
buf.writeU8(0x06) // tag
buf.writeU8(0x01) // length
buf.writeU8(0x02) // predefined = MP4

return buf.Bytes()
}

// =============================================================================
// Codec-specific audio sample entry builders
// =============================================================================
//
// Each audio codec in MP4 requires a specific sample entry box type (e.g., "Opus",
// "fLaC", "ac-3") containing the common AudioSampleEntry fields plus a codec-
// specific configuration box (e.g., dOps, dfLa, dac3). These builders produce
// the inner contents of those boxes.

// buildCommonAudioSampleEntry creates the shared AudioSampleEntry prefix defined
// in ISO 14496-12 §12.2.3. All audio sample entries (mp4a, Opus, fLaC, etc.)
// start with these fields before their codec-specific config box.
func (r *MP4Recorder) buildCommonAudioSampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
entry := newMP4BoxBuilder()
entry.writeBytes(make([]byte, 6)) // reserved (6 bytes, must be zero)
entry.writeU16(1)                  // data_reference_index (1 = self-contained)
entry.writeU32(0)                  // reserved
entry.writeU32(0)                  // reserved
entry.writeU16(channels)           // channel_count
entry.writeU16(16)                 // sample_size (bits per sample)
entry.writeU16(0)                  // pre_defined (must be zero)
entry.writeU16(0)                  // reserved
entry.writeU32(sampleRate << 16)   // sample_rate as fixed-point 16.16
return entry
}

// buildAACSampleEntry creates an mp4a sample entry with an esds box containing
// the AAC AudioSpecificConfig. This is the original path used for AAC audio.
func (r *MP4Recorder) buildAACSampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
entry := r.buildCommonAudioSampleEntry(sampleRate, channels)
// esds box — Elementary Stream Descriptor with AAC config
esdsBuf := r.buildEsdsBox()
entry.writeBox("esds", esdsBuf)
return entry
}

// buildOpusSampleEntry creates an "Opus" sample entry with a "dOps" box
// (OpusSpecificBox) per RFC 7845 §5.1. Opus in MP4 always uses a 48kHz
// timescale regardless of the input sample rate.
func (r *MP4Recorder) buildOpusSampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
// Opus in MP4 always uses 48kHz timescale per RFC 7845
entry := r.buildCommonAudioSampleEntry(48000, channels)

// Build the dOps (OpusSpecificBox) — describes Opus decoder parameters.
// If we have a full OpusHead from the sequence header (≥19 bytes), parse it
// for accurate values. Otherwise, build a minimal default configuration.
dOps := newMP4BoxBuilder()

if len(r.audioConfig) >= 19 {
// OpusHead format (from Ogg/Opus):
//   Bytes 0-7:  "OpusHead" magic string
//   Byte 8:     Version (should be 1)
//   Byte 9:     Output channel count
//   Bytes 10-11: Pre-skip (little-endian uint16)
//   Bytes 12-15: Input sample rate (little-endian uint32)
//   Bytes 16-17: Output gain (little-endian int16)
//   Byte 18:     Channel mapping family
dOps.writeU8(0)                                                                // Version of dOps box
dOps.writeU8(r.audioConfig[9])                                                 // OutputChannelCount
dOps.writeU16(uint16(r.audioConfig[10]) | uint16(r.audioConfig[11])<<8)        // PreSkip (LE→BE)
dOps.writeU32(uint32(r.audioConfig[12]) | uint32(r.audioConfig[13])<<8 |
uint32(r.audioConfig[14])<<16 | uint32(r.audioConfig[15])<<24)                 // InputSampleRate (LE→BE)
dOps.writeU16(uint16(r.audioConfig[16]) | uint16(r.audioConfig[17])<<8)        // OutputGain (LE→BE)
dOps.writeU8(r.audioConfig[18])                                                // ChannelMappingFamily
} else {
// Minimal dOps for when no OpusHead config is available
dOps.writeU8(0)                 // Version
dOps.writeU8(uint8(channels))   // OutputChannelCount
dOps.writeU16(0)                // PreSkip (default 0)
dOps.writeU32(48000)            // InputSampleRate
dOps.writeU16(0)                // OutputGain (0 dB)
dOps.writeU8(0)                 // ChannelMappingFamily (mono/stereo)
}

entry.writeBox("dOps", dOps.Bytes())
return entry
}

// buildFLACSampleEntry creates a "fLaC" sample entry with a "dfLa" box
// containing FLAC METADATA_BLOCK(s). The audioConfig from the sequence header
// should contain the raw FLAC STREAMINFO block (34 bytes).
func (r *MP4Recorder) buildFLACSampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
entry := r.buildCommonAudioSampleEntry(sampleRate, channels)

// dfLa box — FLAC-specific configuration per ISO 14496-12 Amd.3
// Contains a version/flags field followed by FLAC METADATA_BLOCK(s).
dfLa := newMP4BoxBuilder()
dfLa.writeU32(0) // version + flags (always 0)

if len(r.audioConfig) > 0 {
// Write the STREAMINFO as the first (and last) metadata block.
// Block header: 1 byte = [last-block-flag(1 bit) | type(7 bits)]
// followed by 3 bytes of block length.
dfLa.writeU8(0x80)                       // last-block flag set (bit 7=1), type=0 (STREAMINFO)
dfLa.writeU8(0)                           // length high byte
dfLa.writeU16(uint16(len(r.audioConfig))) // length low 2 bytes
dfLa.writeBytes(r.audioConfig)            // raw STREAMINFO data
}

entry.writeBox("dfLa", dfLa.Bytes())
return entry
}

// buildAC3SampleEntry creates an "ac-3" sample entry with a "dac3" box
// (AC3SpecificBox) per ETSI TS 102 366. The audioConfig from the sequence
// header should contain the raw AC-3 specific config data.
func (r *MP4Recorder) buildAC3SampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
entry := r.buildCommonAudioSampleEntry(sampleRate, channels)

// dac3 box — AC3SpecificBox
// If we have config data from the enhanced RTMP sequence header, use it
// directly. Otherwise write a minimal default for 48kHz stereo.
if len(r.audioConfig) > 0 {
entry.writeBox("dac3", r.audioConfig)
} else {
// Minimal dac3: 3 bytes encoding fscod(48kHz), bsid(8), bsmod(complete main),
// acmod(stereo), lfeon(0), bit_rate_code
dac3 := []byte{0x10, 0x40, 0x00}
entry.writeBox("dac3", dac3)
}
return entry
}

// buildEAC3SampleEntry creates an "ec-3" sample entry with a "dec3" box
// (EC3SpecificBox) per ETSI TS 102 366. E-AC-3 extends AC-3 with support
// for more channels and higher bitrates.
func (r *MP4Recorder) buildEAC3SampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
entry := r.buildCommonAudioSampleEntry(sampleRate, channels)

// dec3 box — EC3SpecificBox
if len(r.audioConfig) > 0 {
entry.writeBox("dec3", r.audioConfig)
} else {
// Minimal dec3: 1 independent substream, stereo
dec3 := []byte{0x00, 0x20, 0x0F, 0x00}
entry.writeBox("dec3", dec3)
}
return entry
}

// buildMP3SampleEntry creates a ".mp3" sample entry with an esds box using
// objectTypeIndication 0x6B (MPEG-1 Audio / MP3) instead of 0x40 (AAC).
// MP3 in MP4 is defined in ISO 14496-14 and uses the same ES_Descriptor
// structure as AAC but with a different object type.
func (r *MP4Recorder) buildMP3SampleEntry(sampleRate uint32, channels uint16) *mp4BoxBuilder {
entry := r.buildCommonAudioSampleEntry(sampleRate, channels)

// MP3 doesn't require an AudioSpecificConfig, but if one was provided
// in the sequence header, include it.
asc := r.audioConfig

// Calculate descriptor lengths for the esds structure
decSpecInfoLen := len(asc)
decConfigLen := 13 // fixed DecoderConfigDescriptor fields
if decSpecInfoLen > 0 {
decConfigLen += 2 + decSpecInfoLen // tag + length + data
}
esDescLen := 3 + 2 + decConfigLen + 3 // ES_ID+flags + tag+len + decConfig + SLConfig

esds := newMP4BoxBuilder()
esds.writeU32(0) // version + flags

// ES_Descriptor (tag 0x03)
esds.writeU8(0x03)              // tag
esds.writeU8(uint8(esDescLen))  // length
esds.writeU16(0)                // ES_ID
esds.writeU8(0)                 // flags

// DecoderConfigDescriptor (tag 0x04)
esds.writeU8(0x04)                // tag
esds.writeU8(uint8(decConfigLen)) // length
esds.writeU8(0x6B)                // objectTypeIndication: MPEG-1 Audio (MP3)
esds.writeU8(0x15)                // streamType=5 (audio), upstream=0, reserved=1
esds.writeU8(0x00)                // bufferSizeDB (3 bytes)
esds.writeU16(0x0000)
esds.writeU32(0)                  // maxBitrate
esds.writeU32(0)                  // avgBitrate

// DecoderSpecificInfo (tag 0x05) — only present if config data exists
if decSpecInfoLen > 0 {
esds.writeU8(0x05)                  // tag
esds.writeU8(uint8(decSpecInfoLen)) // length
esds.writeBytes(asc)                // config data
}

// SLConfigDescriptor (tag 0x06)
esds.writeU8(0x06) // tag
esds.writeU8(0x01) // length
esds.writeU8(0x02) // predefined = MP4

entry.writeBox("esds", esds.Bytes())
return entry
}

// =============================================================================
// stts helpers — run-length encode timestamp deltas
// =============================================================================

type sttsEntry struct {
count uint32
delta uint32
}

func videoTimestamps(samples []mp4VideoSample) []uint32 {
ts := make([]uint32, len(samples))
for i, s := range samples {
ts[i] = s.timestamp
}
return ts
}

func buildSttsEntries(timestamps []uint32) []sttsEntry {
if len(timestamps) == 0 {
return nil
}
if len(timestamps) == 1 {
return []sttsEntry{{1, 33}} // default ~30fps
}

var entries []sttsEntry
for i := 1; i < len(timestamps); i++ {
delta := timestamps[i] - timestamps[i-1]
if delta == 0 {
delta = 33 // prevent zero delta
}
if len(entries) > 0 && entries[len(entries)-1].delta == delta {
entries[len(entries)-1].count++
} else {
entries = append(entries, sttsEntry{1, delta})
}
}
// Last sample gets same delta as previous
if len(entries) > 0 {
entries[len(entries)-1].count++
}
return entries
}

// buildAudioSttsEntries creates time-to-sample entries for the audio track.
// Each audio codec has a different standard frame size:
//   - AAC: 1024 samples per frame
//   - Opus: 960 samples per frame (20ms at 48kHz)
//   - MP3: 1152 samples per frame (MPEG-1 Layer III)
//   - FLAC/AC3/EAC3: 1536 samples per frame (common AC-3 frame size at 48kHz)
func buildAudioSttsEntries(samples []mp4AudioSample, sampleRate uint32, audioCodec string) []sttsEntry {
if len(samples) == 0 {
return nil
}

// Select the standard frame duration for this codec
var frameSamples uint32
switch audioCodec {
case "Opus":
frameSamples = 960 // 20ms at 48kHz
case "MP3":
frameSamples = 1152 // MPEG-1 Layer III standard
case "AC3", "EAC3":
frameSamples = 1536 // standard AC-3 frame at 48kHz
case "FLAC":
// FLAC frame sizes vary; use timestamp deltas for accuracy
frameSamples = 0
default: // AAC
frameSamples = 1024
}

// For codecs with a known constant frame size, emit a single run-length entry
if frameSamples > 0 {
return []sttsEntry{{uint32(len(samples)), frameSamples}}
}

// For variable-frame codecs (FLAC), compute deltas from timestamps
if len(samples) == 1 {
// Single sample — use a reasonable default
return []sttsEntry{{1, 1024}}
}
var entries []sttsEntry
for i := 1; i < len(samples); i++ {
delta := uint32(float64(samples[i].timestamp-samples[i-1].timestamp) * float64(sampleRate) / 1000)
if delta == 0 {
delta = 1024 // prevent zero delta
}
if len(entries) > 0 && entries[len(entries)-1].delta == delta {
entries[len(entries)-1].count++
} else {
entries = append(entries, sttsEntry{1, delta})
}
}
// Last sample gets same delta as previous
if len(entries) > 0 {
entries[len(entries)-1].count++
}
return entries
}

// =============================================================================
// mp4BoxBuilder and utility functions
// =============================================================================

func (r *MP4Recorder) writeBoxHeader(w io.Writer, boxType string, data []byte) error {
var header [8]byte
binary.BigEndian.PutUint32(header[:], uint32(8+len(data)))
copy(header[4:], []byte(boxType))
if _, err := w.Write(header[:]); err != nil {
return err
}
_, err := w.Write(data)
return err
}

type mp4BoxBuilder struct {
data []byte
}

func newMP4BoxBuilder() *mp4BoxBuilder {
return &mp4BoxBuilder{data: make([]byte, 0, 1024)}
}

func (b *mp4BoxBuilder) Bytes() []byte {
return b.data
}

func (b *mp4BoxBuilder) writeU8(v uint8) {
b.data = append(b.data, v)
}

func (b *mp4BoxBuilder) writeU16(v uint16) {
var buf [2]byte
binary.BigEndian.PutUint16(buf[:], v)
b.data = append(b.data, buf[:]...)
}

func (b *mp4BoxBuilder) writeU32(v uint32) {
var buf [4]byte
binary.BigEndian.PutUint32(buf[:], v)
b.data = append(b.data, buf[:]...)
}

func (b *mp4BoxBuilder) writeBytes(p []byte) {
b.data = append(b.data, p...)
}

func (b *mp4BoxBuilder) writeBox(boxType string, contents []byte) {
var header [8]byte
binary.BigEndian.PutUint32(header[:], uint32(8+len(contents)))
copy(header[4:], []byte(boxType))
b.data = append(b.data, header[:]...)
b.data = append(b.data, contents...)
}

func identityMatrix() []byte {
return []byte{
0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00,
}
}
