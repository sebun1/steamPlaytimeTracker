package sptt

import (
	"bytes"
	"os"
)

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func GetEnv(filename string) map[string]string {
	file, err := os.Open(filename)
	check(err)

	defer file.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(file)
	env := make(map[string]string)
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		pair := bytes.Split(line, []byte("="))
		env[string(pair[0])] = string(pair[1])
	}
	return env
}
