package protocol

import (
	"bufio"
	"net/http"
)

func ScanSSELines(resp *http.Response, onLine func([]byte) bool) error {
	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)
	for scanner.Scan() {
		if !onLine(scanner.Bytes()) {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
