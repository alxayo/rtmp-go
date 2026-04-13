Start the server

./rtmp-server \
   -listen 192.168.0.12:1935 \
   -srt-listen 192.168.0.12:4200 \
   -record-all true \
   -record-dir ./recordings \
   -metrics-addr :8080 \
   -log-level debug


ffmpeg -f avfoundation -framerate 30 -video_size 1280x720 -i "0:none" \
  -c:v libx264 -preset veryfast -tune zerolatency -pix_fmt yuv420p \
  -f flv rtmp://192.168.0.12:1935/live/test

ffplay -fflags nobuffer -flags low_delay -framedrop -probesize 32 -analyzeduration 0 -sync ext rtmp://192.168.0.12:1935/live/test

ffplay -fflags nobuffer -flags low_delay -framedrop -probesize 32 -analyzeduration 0 -sync ext -rtmp_live live rtmp://192.168.0.12:1935/live/test

Use SRT:
fmpeg -f avfoundation -framerate 30 -video_size 1280x720 -i "0:none" \
  -an -c:v libx264 -preset veryfast -tune zerolatency -pix_fmt yuv420p \
  -f mpegts "srt://192.168.0.12:4200?streamid=publish:live/test"


  Use TLS:
  ./rtmp-server \
  -listen 192.168.0.12:1935 \
  -tls-listen 192.168.0.12:1936 \
  -tls-cert cert.pem \
  -tls-key key.pem \
  -srt-listen 192.168.0.12:4200 \
  -record-all true \
  -record-dir ./recordings \
  -metrics-addr :8080 \
  -log-level debug