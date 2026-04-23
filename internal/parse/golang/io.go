package golang

import "os"

func readFileOS(path string) ([]byte, error) { return os.ReadFile(path) }
