//go:build !unix

package app

func prepareKeyFile(keyPath, dbPath string) error {
	return nil
}
