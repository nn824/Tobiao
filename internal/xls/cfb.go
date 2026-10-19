// Package xls 实现一个零依赖的 BIFF8（Excel 97-2003 .xls）写出器。
//
// 仅覆盖本工具需要的子集：单工作表、合并单元格、行高、列宽、
// 共享字符串表（SST）、文本与数字单元格、边框与对齐样式。
// 所有实现均以 MS-XLS 规范与真实 .xls 文件的记录布局为准。
package xls

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	cfbSectorSize = 512
	cfbDirEntry   = 128
	// 迷你流阈值：小于该值的流应放入迷你流。本实现不使用迷你流，
	// 因为 Workbook 流远大于 4096 字节。
	cfbMiniCutoff = 0x1000

	sidFree        uint32 = 0xFFFFFFFF
	sidEndOfChain  uint32 = 0xFFFFFFFE
	sidUsedBySAT   uint32 = 0xFFFFFFFD
	sidUsedByDIFAT uint32 = 0xFFFFFFFC
)

// WriteCFB 将名为 streamName 的单个流封装为 OLE2 复合文档（.xls 的容器格式）。
//
// 物理布局：
//
//	偏移 0            512 字节文件头（含前 109 项 DIFAT）
//	扇区 0 .. B-1     流数据（按 512 字节对齐）
//	扇区 B .. B+M-1   DIFAT（MSAT）追加扇区，仅当 FAT 超过 109 个扇区时出现
//	扇区 ...          FAT 扇区
//	扇区 末尾         目录扇区
//
// 关键约束（OLE2 规范，读取器会严格校验）：流的 FAT 链长度必须**恰好**等于
// ceil(流字节数/512)。多分配（例如把流补齐到 4096 边界）会让 xlrd 报
// "size exceeds expected N bytes; corrupt?"，因此这里只按 512 字节对齐。
func WriteCFB(w io.Writer, streamName string, stream []byte) error {
	streamLen := uint64(len(stream))

	// 1) 流按 512 字节对齐（只补齐到扇区边界）。
	bookSectCount := uint32((len(stream) + cfbSectorSize - 1) / cfbSectorSize)
	if bookSectCount == 0 {
		bookSectCount = 1
	}
	padded := make([]byte, int(bookSectCount)*cfbSectorSize)
	copy(padded, stream)

	dirStream := buildDirectory(streamName, streamLen)
	dirSectCount := uint32((len(dirStream) + cfbSectorSize - 1) / cfbSectorSize)

	// 2) 自洽求解 FAT 扇区数与 DIFAT 追加扇区数（FAT 自身也占扇区，需迭代到稳定）：
	//      sat  = ceil((book + dir + sat + msat) / 128)   （每扇区 128 个 SID）
	//      msat = ceil(max(0, sat - 109) / 127)           （每扇区 127 个 SID + 1 个链指针）
	var satSectCount, msatSectCount uint32
	for i := 0; i < 128; i++ {
		total := bookSectCount + dirSectCount + satSectCount + msatSectCount
		needSat := (total + 127) / 128
		var needMsat uint32
		if needSat > 109 {
			needMsat = (needSat - 109 + 126) / 127
		}
		if needSat == satSectCount && needMsat == msatSectCount {
			break
		}
		satSectCount, msatSectCount = needSat, needMsat
	}
	if satSectCount > 109+127*msatSectCount {
		return fmt.Errorf("工作簿过大：需要 %d 个 FAT 扇区（DIFAT 容量不足）", satSectCount)
	}

	// 3) 分配扇区编号并同时构建 FAT。
	sat := make([]uint32, 128*satSectCount)
	for i := range sat {
		sat[i] = sidFree
	}

	sect := uint32(0)
	// allocate 连续分配 n 个扇区；mark==0 表示作为普通流写成链（末项 EOC），
	// 否则把每个扇区在 FAT 中标记为 mark（FAT/DIFAT 扇区自身）。
	allocate := func(n uint32, mark uint32) []uint32 {
		out := make([]uint32, 0, n)
		for i := uint32(0); i < n; i++ {
			out = append(out, sect)
			switch {
			case mark != 0:
				sat[sect] = mark
			case i+1 < n:
				sat[sect] = sect + 1
			default:
				sat[sect] = sidEndOfChain
			}
			sect++
		}
		return out
	}

	allocate(bookSectCount, 0)                          // 工作簿流
	msatSect := allocate(msatSectCount, sidUsedByDIFAT) // DIFAT 追加扇区
	satSect := allocate(satSectCount, sidUsedBySAT)     // FAT 扇区
	dirSect := allocate(dirSectCount, 0)                // 目录流

	// 4) 文件头。
	header := make([]byte, cfbSectorSize)
	copy(header[0:], []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	// 8..23  CLSID 全 0
	binary.LittleEndian.PutUint16(header[24:], 0x003E) // 修订号
	binary.LittleEndian.PutUint16(header[26:], 0x0003) // 版本号
	binary.LittleEndian.PutUint16(header[28:], 0xFFFE) // 字节序标记
	binary.LittleEndian.PutUint16(header[30:], 9)      // 扇区大小 2^9
	binary.LittleEndian.PutUint16(header[32:], 6)      // 迷你扇区大小 2^6
	// 34..43 保留
	binary.LittleEndian.PutUint32(header[40:], 0)             // 目录扇区数（版本 3 必须为 0）
	binary.LittleEndian.PutUint32(header[44:], satSectCount)  // FAT 扇区数
	binary.LittleEndian.PutUint32(header[48:], dirSect[0])    // 目录起始扇区
	binary.LittleEndian.PutUint32(header[52:], 0)             // 事务签名
	binary.LittleEndian.PutUint32(header[56:], cfbMiniCutoff) // 迷你流阈值
	binary.LittleEndian.PutUint32(header[60:], sidEndOfChain) // 无迷你 FAT
	binary.LittleEndian.PutUint32(header[64:], 0)             // 迷你 FAT 扇区数
	if len(msatSect) == 0 {
		binary.LittleEndian.PutUint32(header[68:], sidEndOfChain) // DIFAT 起始扇区
	} else {
		binary.LittleEndian.PutUint32(header[68:], msatSect[0])
	}
	binary.LittleEndian.PutUint32(header[72:], uint32(len(msatSect))) // DIFAT 扇区数
	for i := 0; i < 109; i++ {
		v := sidFree
		if i < len(satSect) {
			v = satSect[i]
		}
		binary.LittleEndian.PutUint32(header[76+i*4:], v)
	}

	if _, err := w.Write(header); err != nil {
		return err
	}
	if _, err := w.Write(padded); err != nil {
		return err
	}
	// DIFAT 追加扇区（仅当 FAT 超过 109 个扇区时出现）：
	// 每扇区存 127 个 FAT 扇区号，最后 4 字节是下一个 DIFAT 扇区的扇区号。
	if len(msatSect) > 0 {
		buf := make([]byte, int(msatSectCount)*cfbSectorSize)
		extra := satSect
		if len(extra) > 109 {
			extra = extra[109:]
		} else {
			extra = nil
		}
		for i := 0; i < int(msatSectCount); i++ {
			base := i * cfbSectorSize
			for j := 0; j < 127; j++ {
				v := sidFree
				if k := i*127 + j; k < len(extra) {
					v = extra[k]
				}
				binary.LittleEndian.PutUint32(buf[base+j*4:], v)
			}
			next := sidEndOfChain
			if i+1 < len(msatSect) {
				next = msatSect[i+1]
			}
			binary.LittleEndian.PutUint32(buf[base+127*4:], next)
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}
	// FAT
	satBuf := make([]byte, len(sat)*4)
	for i, v := range sat {
		binary.LittleEndian.PutUint32(satBuf[i*4:], v)
	}
	if _, err := w.Write(satBuf); err != nil {
		return err
	}
	if _, err := w.Write(dirStream); err != nil {
		return err
	}
	return nil
}

func buildDirectory(streamName string, streamLen uint64) []byte {
	buf := &bytes.Buffer{}
	// 目录项 0：Root Entry
	writeDirEntry(buf, "Root Entry", 0x05, 1, sidFree, sidFree, 1, sidEndOfChain, 0)
	// 目录项 1：Workbook 流
	writeDirEntry(buf, streamName, 0x02, 1, sidFree, sidFree, sidFree, 0, streamLen)
	// 目录项 2、3：空项，使目录流对齐到一个完整扇区
	for i := 0; i < 2; i++ {
		writeDirEntry(buf, "", 0x00, 1, sidFree, sidFree, sidFree, sidEndOfChain, 0)
	}
	return buf.Bytes()
}

func writeDirEntry(buf *bytes.Buffer, name string, objType, colour byte, left, right, child, start uint32, size uint64) {
	raw := make([]byte, 0, 64)
	if name != "" {
		u := utf16LE(name)
		raw = append(raw, u...)
		raw = append(raw, 0, 0) // 结尾 NUL
	}
	nameLen := len(raw)
	padded := make([]byte, 64)
	copy(padded, raw)

	buf.Write(padded)
	_ = binary.Write(buf, binary.LittleEndian, uint16(nameLen))
	buf.WriteByte(objType)
	buf.WriteByte(colour)
	_ = binary.Write(buf, binary.LittleEndian, int32(left))
	_ = binary.Write(buf, binary.LittleEndian, int32(right))
	_ = binary.Write(buf, binary.LittleEndian, int32(child))
	buf.Write(make([]byte, 16)) // CLSID
	buf.Write(make([]byte, 4))  // 状态位
	buf.Write(make([]byte, 8))  // 创建时间
	buf.Write(make([]byte, 8))  // 修改时间
	_ = binary.Write(buf, binary.LittleEndian, start)
	_ = binary.Write(buf, binary.LittleEndian, size)
	if buf.Len()%cfbDirEntry != 0 {
		panic(fmt.Sprintf("目录项长度异常: %d", buf.Len()))
	}
}

func utf16LE(s string) []byte {
	return encodeUTF16(s)
}
