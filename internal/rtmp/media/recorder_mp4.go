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
videoCodec   string           // detected codec: "H265", "H264", etc.
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
// Other audio codecs (MP3, etc.) are not supported in MP4 recorder
}

// handleEnhancedAudio processes Enhanced RTMP audio messages.
func (r *MP4Recorder) handleEnhancedAudio(msg *chunk.Message, data []byte) {
if len(data) < 5 {
return
}

pktType := data[0] & 0x0F

switch pktType {
case 0: // SequenceStart — AudioSpecificConfig after FourCC
r.audioConfig = make([]byte, len(data[5:]))
copy(r.audioConfig, data[5:])
case 1: // CodedFrames — raw audio after FourCC
r.writeAudioSample(data[5:], msg.Timestamp)
}
}

// handleLegacyAAC processes traditional RTMP AAC audio messages.
func (r *MP4Recorder) handleLegacyAAC(msg *chunk.Message, data []byte) {
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
sampleRate := uint32(44100)
channels := uint16(2)
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

// Codec configuration box (hvcC or avcC) with actual config from sequence header
configBoxType := "hvcC"
if r.videoCodec == "H264" {
configBoxType = "avcC"
}
if len(r.videoConfig) > 0 {
sampleEntry.writeBox(configBoxType, r.videoConfig)
} else {
// Fallback: empty config (file may not be fully playable)
sampleEntry.writeBox(configBoxType, []byte{})
}

entryBoxType := "hvc1"
if r.videoCodec == "H264" {
entryBoxType = "avc1"
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
func (r *MP4Recorder) buildAudioSampleTable(sampleRate uint32, channels uint16) *mp4BoxBuilder {
stblBuf := newMP4BoxBuilder()
samples := r.audioSamples

// --- stsd ---
stsdBuf := newMP4BoxBuilder()
stsdBuf.writeU32(0) // version + flags
stsdBuf.writeU32(1) // entry_count

sampleEntry := newMP4BoxBuilder()
sampleEntry.writeBytes(make([]byte, 6)) // reserved
sampleEntry.writeU16(1)                 // data_reference_index
// Audio sample entry fields (ISO 14496-12)
sampleEntry.writeU32(0)         // reserved
sampleEntry.writeU32(0)         // reserved
sampleEntry.writeU16(channels)  // channel_count
sampleEntry.writeU16(16)        // sample_size (bits)
sampleEntry.writeU16(0)         // pre_defined
sampleEntry.writeU16(0)         // reserved
sampleEntry.writeU32(sampleRate << 16) // sample_rate (fixed-point 16.16)

// esds box with actual AudioSpecificConfig
esdsBuf := r.buildEsdsBox()
sampleEntry.writeBox("esds", esdsBuf)

stsdBuf.writeBox("mp4a", sampleEntry.Bytes())
stblBuf.writeBox("stsd", stsdBuf.Bytes())

// --- stts ---
// For AAC, each frame is typically 1024 samples at the given sample rate.
// Use actual timestamps converted to audio timescale.
sttsEntries := buildAudioSttsEntries(samples, sampleRate)
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

func buildAudioSttsEntries(samples []mp4AudioSample, sampleRate uint32) []sttsEntry {
if len(samples) == 0 {
return nil
}
// AAC frames are typically 1024 samples per frame at the given sample rate
// Use constant delta for simplicity (most AAC streams are CBR-timed)
aacFrameSamples := uint32(1024)
return []sttsEntry{{uint32(len(samples)), aacFrameSamples}}
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
