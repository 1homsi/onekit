package gendart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const plainDateSchema = `namespace app;

model Booking {
  day: plainDate;
  days: plainDate[];
  byCity: Record<plainDate>;
}
`

// A plainDate is the calendar day the caller holds. Encoding it through
// toUtc() sent local midnight east of UTC as the day before, and a late
// evening west of UTC as the day after. A list or map of them sent each item
// as a full timestamp.
func TestDartPlainDateSendsTheDayItHolds(t *testing.T) {
	dir := dartPackage(t, compileDartSchema(t, plainDateSchema))
	writeDartFile(t, filepath.Join(dir, "bin", "main.dart"), `
import 'dart:convert';
import 'package:onekit_check/models.dart';

void main() {
  if (DateTime(2026, 11, 3).timeZoneOffset == Duration.zero) {
    print('UTC');
    return;
  }
  const want = '{"day":"2026-11-03","days":["2026-11-03"],"byCity":{"x":"2026-11-03"}}';
  for (final day in [DateTime(2026, 11, 3), DateTime(2026, 11, 3, 23, 59), DateTime.utc(2026, 11, 3)]) {
    final wire = jsonEncode(Booking(day: day, days: [day], byCity: {'x': day}).toJson());
    if (wire != want) throw '$day (UTC${day.timeZoneOffset}) sent as $wire';
  }
  final back = Booking.fromJson(jsonDecode(want) as Map<String, dynamic>);
  if (jsonEncode(back.toJson()) != want) throw 'decoded ${back.day} sent as ${back.toJson()}';
  print('OK');
}
`)
	// UTC+4 and UTC-10 in the POSIX form, which glibc, macOS and the Windows C
	// runtime all read from TZ without a zone database.
	tested := false
	for _, zone := range []string{"GST-4", "HST10"} {
		cmd := exec.Command("dart", "run", "bin/main.dart")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TZ="+zone)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("TZ=%s dart run: %v\n%s", zone, err, out)
		}
		switch strings.TrimSpace(string(out)) {
		case "OK":
			tested = true
		case "UTC":
		default:
			t.Fatalf("TZ=%s dart run: %s", zone, out)
		}
	}
	if !tested {
		t.Skip("dart ran on UTC in every zone, where a plain date cannot shift")
	}
}
