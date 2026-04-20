package docker

import (
	"bytes"
	"encoding/binary"
	"io"
)

// demuxStream reads a Docker multiplexed stream and separates stdout and
// stderr. The stream uses 8-byte headers: byte 0 is the stream type
// (1=stdout, 2=stderr), bytes 4-7 are a big-endian uint32 frame size.
//
// At most maxBytes of combined output are collected. If the stream exceeds
// this limit, remaining data is drained to avoid connection issues.
func demuxStream(r io.Reader, maxBytes int) (stdout, stderr string, err error) {
	var outBuf, errBuf bytes.Buffer
	header := make([]byte, 8)
	total := 0

	for {
		_, err := io.ReadFull(r, header)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// If we read 0 bytes it's a clean EOF; if partial it's malformed.
				// io.ReadFull returns io.EOF only when 0 bytes were read,
				// and io.ErrUnexpectedEOF when some but not all were read.
				if err == io.ErrUnexpectedEOF {
					return "", "", err
				}
				break
			}
			return "", "", err
		}

		streamType := header[0]
		frameSize := int(binary.BigEndian.Uint32(header[4:8]))

		// Truncate frame to remaining capacity.
		toRead := frameSize
		if total+toRead > maxBytes {
			toRead = maxBytes - total
		}

		if toRead > 0 {
			payload := make([]byte, toRead)
			if _, err := io.ReadFull(r, payload); err != nil {
				return "", "", err
			}
			switch streamType {
			case 1:
				outBuf.Write(payload)
			case 2:
				errBuf.Write(payload)
			}
		}

		// Skip remainder of frame if truncated.
		skip := frameSize - toRead
		if skip > 0 {
			if _, err := io.CopyN(io.Discard, r, int64(skip)); err != nil {
				// Best effort drain; ignore errors.
				break
			}
		}

		total += toRead
		if total >= maxBytes {
			// Drain remaining stream data.
			_, _ = io.Copy(io.Discard, r)
			break
		}
	}

	return outBuf.String(), errBuf.String(), nil
}
