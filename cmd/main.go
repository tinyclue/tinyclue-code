package main

import (
	"github.com/tinyclue/tinyclue-code/coding_agent"
)

func main() {
	ia := codingagent.New()
	ia.Init()
	ia.Start()
}
