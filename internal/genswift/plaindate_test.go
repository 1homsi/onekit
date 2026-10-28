package genswift

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A plainDate is the calendar day the caller holds, read in the device's time
// zone. Encoding it as the UTC day sent local midnight east of UTC (a
// DatePicker value) as the day before, and a late evening west of UTC as the
// day after; decoding to UTC midnight made a received day read as the day
// before west of UTC.
func TestSwiftPlainDateSendsTheDayItHolds(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the Swift client targets Apple platforms")
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		t.Skip("swiftc not available")
	}
	file := compileSwiftSchema(t, `namespace app;

model Booking {
  day: plainDate;
}
`)
	dir := t.TempDir()
	writeSwiftFile(t, filepath.Join(dir, "Onekit.swift"), string(GenerateRuntime()))
	writeSwiftFile(t, filepath.Join(dir, "Models.swift"), string(GenerateTypes(file)))
	writeSwiftFile(t, filepath.Join(dir, "main.swift"), `import Foundation

if String(TimeZone.current.secondsFromGMT()) != CommandLine.arguments[1] {
    print("IGNORED")
    exit(0)
}
func wire(_ booking: Booking) -> String {
    let data = try! JSONSerialization.data(withJSONObject: booking.toJSONValue(), options: [.sortedKeys])
    return String(decoding: data, as: UTF8.self)
}
var calendar = Calendar(identifier: .gregorian)
calendar.timeZone = TimeZone.current
let midnight = calendar.date(from: DateComponents(year: 2026, month: 11, day: 3))!
let evening = calendar.date(from: DateComponents(year: 2026, month: 11, day: 3, hour: 23, minute: 59))!
for day in [midnight, evening] {
    let sent = wire(Booking(day: day))
    precondition(sent == "{\"day\":\"2026-11-03\"}", "\(day) in \(TimeZone.current.identifier) sent as \(sent)")
}
let back = try Booking(json: ["day": "2026-11-03"])
precondition(back.day.map { calendar.dateComponents([.year, .month, .day], from: $0) } == DateComponents(year: 2026, month: 11, day: 3),
             "decoded \(String(describing: back.day)) in \(TimeZone.current.identifier)")
let again = wire(back)
precondition(again == "{\"day\":\"2026-11-03\"}", "decoded \(String(describing: back.day)) sent as \(again)")
print("OK")
`)
	bin := filepath.Join(dir, "check")
	compile := exec.Command("swiftc", "-warnings-as-errors", "-o", bin, "Onekit.swift", "Models.swift", "main.swift")
	compile.Dir = dir
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("swiftc: %v\n%s", err, out)
	}
	// UTC+4 and UTC-10, neither with daylight saving time. Foundation reads
	// TZ as a zone database name only: with the POSIX form (GST-4) it keeps
	// the system zone.
	tested := false
	for _, zone := range []struct{ name, offset string }{{"Asia/Dubai", "14400"}, {"Pacific/Honolulu", "-36000"}} {
		cmd := exec.Command(bin, zone.offset)
		cmd.Env = append(os.Environ(), "TZ="+zone.name)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("TZ=%s: %v\n%s", zone.name, err, out)
		}
		switch strings.TrimSpace(string(out)) {
		case "OK":
			tested = true
		case "IGNORED":
		default:
			t.Fatalf("TZ=%s: %s", zone.name, out)
		}
	}
	if !tested {
		t.Skip("Foundation ignored TZ for every zone")
	}
}
