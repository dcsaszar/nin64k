package pipeline

import (
	"os"
	"path/filepath"
)

type Config struct {
	ProjectRoot string
	OutputDir   string
	PartTimes   []int
}

func (c *Config) ProjectPath(rel string) string {
	return filepath.Join(c.ProjectRoot, rel)
}

func FindProjectRoot() string {
	if wd, err := os.Getwd(); err == nil {
		for d := wd; d != "/" && d != "."; d = filepath.Dir(d) {
			if _, err := os.Stat(filepath.Join(d, "src/odin_player.inc")); err == nil {
				return d
			}
		}
	}
	return "../.."
}

var DefaultPartTimes = []int{
	0xBB44, 0x7234, 0x57C0, 0x88D0, 0xC0A4, 0x79F6, 0x491A, 0x7BF0, 0x6D80,
}
