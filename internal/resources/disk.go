package resources

import "fmt"

func Check(path string, reserve uint64) error {
	free, err := Free(path)
	if err != nil {
		return err
	}
	if free < reserve {
		return fmt.Errorf("insufficient disk space: %d bytes free; %d bytes reserved", free, reserve)
	}
	return nil
}
