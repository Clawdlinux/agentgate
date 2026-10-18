import contextlib
import io
import json
import sys
import unittest
from unittest.mock import patch

import analyze


class AnalyzeTest(unittest.TestCase):
    def test_nearest_rank(self):
        self.assertEqual(analyze.quantiles([4e6, 1e6, 2e6, 3e6]),
                         {"p50_ms": 2, "p99_ms": 4, "max_ms": 4})

    def test_correlated_receipt_phases(self):
        samples = (
            "total_ns,upstream_ns,dispatch_ns,auth_ns,receipt_ns,handler_ns,client_conn_ns\n"
            "500,100,10,20,200,300,50\n"
            "1000,100,10,20,500,800,50\n"
        )
        events = [
            'G=1 Log Time=0 Task=7 Category="request.id" Message="1"',
            'G=1 Log Time=20 Task=7 Category="upstream.getconn" Message=""',
            'G=1 Log Time=70 Task=7 Category="upstream.gotconn" Message="true"',
            'G=1 RegionBegin Time=100 Task=7 Type="receipt.append"',
            'G=2 RegionBegin Time=200 Task=0 Type="receipt.transaction"',
            'G=2 RegionBegin Time=210 Task=0 Type="receipt.head"',
            'G=2 RegionEnd Time=220 Task=0 Type="receipt.head"',
            'G=2 RegionBegin Time=230 Task=0 Type="receipt.sign"',
            'G=2 RegionEnd Time=240 Task=0 Type="receipt.sign"',
            'G=2 RegionBegin Time=250 Task=0 Type="receipt.insert"',
            'G=2 RegionEnd Time=260 Task=0 Type="receipt.insert"',
            'G=2 RegionBegin Time=300 Task=0 Type="receipt.commit"',
            'G=3 RangeBegin Time=350 Name="stop-the-world (GC mark termination)" Scope=Goroutine(3)',
            'G=3 RangeEnd Time=360 Name="stop-the-world (GC mark termination)" Scope=Goroutine(3)',
            'G=2 RegionEnd Time=450 Task=0 Type="receipt.commit"',
            'G=2 RegionEnd Time=500 Task=0 Type="receipt.transaction"',
            'G=2 Log Time=510 Task=7 Category="receipt.reply" Message=""',
            'G=1 RegionEnd Time=600 Task=7 Type="receipt.append"',
        ]
        trace = "\n".join("M=1 P=1 " + event for event in events)
        output = io.StringIO()

        def open_fixture(path):
            return io.StringIO(samples if path == "samples" else trace)

        with patch.object(sys, "argv", ["analyze", "samples", "--trace", "trace"]):
            with patch("builtins.open", side_effect=open_fixture):
                with contextlib.redirect_stdout(output):
                    analyze.main()
        result = json.loads(output.getvalue())
        self.assertEqual(result["tail_count"], 1)
        self.assertEqual(result["tail_mean_ms"], .0009)
        self.assertEqual(result["tail_receipt_trace"]["matched"], 1)
        phases = result["tail_receipt_trace"]["mean_ms"]
        self.assertEqual(phases["queue_and_writer_acquire"], .0001)
        self.assertEqual(phases["ack_schedule"], .0001)
        self.assertEqual(phases["receipt.commit"], .00015)
        self.assertEqual(phases["overlapping_gc_stw"], .00001)
        self.assertEqual(result["upstream_connections"]["reuse_pct"], 100)
        self.assertEqual(result["upstream_connections"]["p99_ms"], .00005)
        self.assertEqual(result["gc_stw"]["count"], 1)


if __name__ == "__main__":
    unittest.main()
