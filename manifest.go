package stretcher

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"gopkg.in/yaml.v1"
	"hash"
	"log"
	"os"
	"os/exec"
	"io/ioutil"
	"strings"
)

type Manifest struct {
	Src      string   `yaml:"src"`
	CheckSum string   `yaml:"checksum"`
	Dest     string   `yaml:"dest"`
	Commands Commands `yaml:"commands"`
}

type Commands struct {
	Pre  []string `yaml:"pre"`
	Post []string `yaml:"post"`
}

func (m *Manifest) InvokePreDeployCommands() error {
	for _, comm := range m.Commands.Pre {
		log.Println("invoking pre deploy command:", comm)
		out, err := exec.Command("sh", "-c", comm).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed:", comm, err)
		}
		fmt.Println(string(out))
	}
	return nil
}

func (m *Manifest) InvokePostDeployCommands() error {
	for _, comm := range m.Commands.Post {
		log.Println("invoking post deploy command:", comm)
		out, err := exec.Command("sh", "-c", comm).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed:", comm, err)
		}
		fmt.Println(string(out))
	}
	return nil
}

func (m *Manifest) newHash() (hash.Hash, error) {
	switch len(m.CheckSum) {
	case 32:
		return md5.New(), nil
	case 40:
		return sha1.New(), nil
	case 64:
		return sha256.New(), nil
	case 128:
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("checksum must be md5, sha1, sha256, sha512 hex string.")
	}
}

func (m *Manifest) Deploy() error {
	src, err := getURL(m.Src)
	if err != nil {
		return fmt.Errorf("Get src failed:", err)
	}
	defer src.Close()

	tmp, err := ioutil.TempFile(os.TempDir(), "stretcher")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	written, sum, err := m.copyAndCalcHash(tmp, src)
	tmp.Close()
	if err != nil {
		return err
	}
	log.Printf("Wrote %d bytes to %s", written, tmp.Name())
	if len(m.CheckSum) > 0 && sum != strings.ToLower(m.CheckSum) {
		return fmt.Errorf("Checksum mismatch. expected:%s got:%s", m.CheckSum, sum)
	} else {
		log.Printf("Checksum ok: %s", sum)
	}

	dir, err := ioutil.TempDir(os.TempDir(), "stretcher_src")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	err = m.InvokePreDeployCommands()
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	if err = os.Chdir(dir); err != nil {
		return err
	}

	log.Println("Extract archive:", tmp.Name(), "to", dir)
	out, err := exec.Command("tar", "xf", tmp.Name()).CombinedOutput()
	if err != nil {
		log.Println("failed: tar xf", tmp.Name(), "failed", err)
		return err
	}
	fmt.Println(string(out))

	from := dir + "/"
	to := m.Dest
	// append "/" when not terminated by "/"
	if strings.LastIndex(to, "/") != len(to)-1 {
		to = to + "/"
	}

	log.Println("rsync -av --delete", from, to)
	out, err = exec.Command("rsync", "-av", "--delete", from, to).CombinedOutput()
	if err != nil {
		log.Println("failed: rsync -av --delete", from, to)
		return err
	}
	fmt.Println(string(out))

	if err = os.Chdir(cwd); err != nil {
		return err
	}

	err = m.InvokePostDeployCommands()
	if err != nil {
		return err
	}
	return nil
}

func (m *Manifest) copyAndCalcHash(dst io.Writer, src io.Reader) (written int64, sum string, err error) {
	h, err := m.newHash()
	if err != nil {
		return int64(0), "", err
	}
	buf := make([]byte, 32*1024)
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			io.WriteString(h, string(buf[0:nr]))
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			err = er
			break
		}
	}
	s := fmt.Sprintf("%x", h.Sum(nil))
	return written, s, err
}

func ParseManifest(data []byte) (*Manifest, error) {
	m := &Manifest{}
	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, err
	}
	if m.Src == "" {
		return nil, fmt.Errorf("Src is required")
	}
	if m.Dest == "" {
		return nil, fmt.Errorf("Dest is required")
	}
	return m, nil
}