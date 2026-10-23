package iso9660

import (
	"fmt"
	"io/fs"
	"strings"
)

// Rock Ridge's continuation area is metadata, not a file payload. Bound the
// total bytes and chain depth before allocating any continuation buffer.
const maxContinuation = 1 << 20

func (f *filesystem) rockRidgeEntries(n *node, initial []byte) error {
	queue := [][]byte{initial}
	visited := map[extent]bool{}
	bytesRead := 0
	var alternate strings.Builder
	var components []string
	var component strings.Builder
	linkSeen := false
	nameSeen, nameContinued, linkContinued := false, false, false
	componentContinued := false
	for areas := 0; len(queue) > 0; areas++ {
		if areas >= 128 {
			return fmt.Errorf("too many Rock Ridge continuation areas")
		}
		area := queue[0]
		queue = queue[1:]
		for len(area) > 0 {
			if area[0] == 0 {
				break
			}
			if len(area) < 4 || area[2] < 4 || int(area[2]) > len(area) || area[3] != 1 {
				return fmt.Errorf("invalid SUSP entry")
			}
			e := area[:int(area[2])]
			area = area[len(e):]
			switch string(e[:2]) {
			case "ST":
				area = nil
			case "NM":
				if len(e) < 5 || e[4]&^byte(1) != 0 {
					return fmt.Errorf("invalid Rock Ridge name")
				}
				if nameSeen && !nameContinued {
					return fmt.Errorf("duplicate Rock Ridge name")
				}
				nameSeen = true
				nameContinued = e[4]&1 != 0
				alternate.Write(e[5:])
			case "PX":
				if len(e) != 36 && len(e) != 44 {
					return fmt.Errorf("invalid Rock Ridge PX length")
				}
				mode, err := number32(e[4:12])
				if err != nil {
					return err
				}
				n.mode = fs.FileMode(mode & 0777)
				if mode&04000 != 0 {
					n.mode |= fs.ModeSetuid
				}
				if mode&02000 != 0 {
					n.mode |= fs.ModeSetgid
				}
				if mode&01000 != 0 {
					n.mode |= fs.ModeSticky
				}
				switch mode & 0170000 {
				case 0040000:
					n.mode |= fs.ModeDir
				case 0120000:
					n.mode |= fs.ModeSymlink
				case 0020000:
					n.mode |= fs.ModeDevice | fs.ModeCharDevice
				case 0060000:
					n.mode |= fs.ModeDevice
				case 0010000:
					n.mode |= fs.ModeNamedPipe
				case 0140000:
					n.mode |= fs.ModeSocket
				}
			case "SL":
				if len(e) < 5 || e[4]&^byte(1) != 0 {
					return fmt.Errorf("invalid Rock Ridge SL")
				}
				if linkSeen && !linkContinued {
					return fmt.Errorf("duplicate Rock Ridge symlink")
				}
				linkSeen = true
				linkContinued = e[4]&1 != 0
				for c := e[5:]; len(c) > 0; {
					if len(c) < 2 || int(c[1]) > len(c)-2 {
						return fmt.Errorf("invalid symlink component")
					}
					flag := c[0]
					text := string(c[2 : 2+int(c[1])])
					c = c[2+int(c[1]):]
					switch flag &^ byte(1) {
					case 0:
					case 2:
						text = "."
					case 4:
						text = ".."
					case 8:
						text = ""
						if len(components) > 0 || component.Len() > 0 {
							return fmt.Errorf("invalid symlink root")
						}
					default:
						return fmt.Errorf("unsupported Rock Ridge symlink component")
					}
					if strings.ContainsAny(text, "/\x00") {
						return fmt.Errorf("invalid symlink component text")
					}
					component.WriteString(text)
					componentContinued = flag&1 != 0
					if flag&1 == 0 {
						components = append(components, component.String())
						component.Reset()
					}
				}
			case "CE":
				if len(e) != 28 {
					return fmt.Errorf("invalid SUSP CE length")
				}
				sector, err := number32(e[4:12])
				if err != nil {
					return err
				}
				offset, err := number32(e[12:20])
				if err != nil {
					return err
				}
				length, err := number32(e[20:28])
				if err != nil {
					return err
				}
				if offset >= uint32(f.block) || length == 0 || length > maxContinuation || int(length) > maxContinuation-bytesRead {
					return fmt.Errorf("invalid or oversized SUSP continuation")
				}
				location := extent{int64(sector)*f.block + int64(offset), int64(length)}
				if visited[location] {
					return fmt.Errorf("cyclic SUSP continuation")
				}
				visited[location] = true
				data := make([]byte, int(length))
				if err := readFull(f.r, data, location.offset, f.limit); err != nil {
					return err
				}
				bytesRead += len(data)
				queue = append(queue, data)
			}
		}
	}
	if alternate.Len() > 0 {
		n.name = alternate.String()
	}
	if nameContinued || linkContinued || componentContinued {
		return fmt.Errorf("unfinished Rock Ridge continuation")
	}
	if component.Len() > 0 {
		return fmt.Errorf("unfinished symlink component")
	}
	if linkSeen {
		n.target = strings.Join(components, "/")
		if len(components) == 1 && components[0] == "" {
			n.target = "/"
		}
		n.mode &^= fs.ModeType
		n.mode |= fs.ModeSymlink
	}
	if n.mode&fs.ModeSymlink != 0 && (!linkSeen || n.target == "") {
		return fmt.Errorf("Rock Ridge symlink lacks target")
	}
	return nil
}
