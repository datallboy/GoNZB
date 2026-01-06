package repair

import "os/exec"

type CLIPar2 struct {
	BinaryPath string
}

func NewCLIPar2() *CLIPar2 {
	return &CLIPar2{BinaryPath: "par2"}
}

func (c *CLIPar2) Verify(path string) (bool, error) {
	// 'v' is verify, '-q' is quiet
	cmd := exec.Command(c.BinaryPath, "v", "-q", path)
	err := cmd.Run()
	if err == nil {
		return true, nil // Exit code 0
	}

	if exitError, ok := err.(*exec.ExitError); ok {
		// Exit code 1
		if exitError.ExitCode() == 1 {
			return false, nil // Damanged but repairable
		}
	}
	return false, err // Hard error or unrepairable (Exit code 2+)
}

func (c *CLIPar2) Repair(path string) error {
	// 'r' is repair
	cmd := exec.Command(c.BinaryPath, "r", path)
	return cmd.Run()
}
