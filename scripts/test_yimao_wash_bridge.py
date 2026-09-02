import importlib.util
import unittest
from pathlib import Path

HERE = Path(__file__).with_name("yimao_wash_bridge.py")
spec = importlib.util.spec_from_file_location("yimao_wash_bridge", HERE)
bridge = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bridge)

class BridgeTests(unittest.TestCase):
    def item(self, **overrides):
        x = {"request_id": "wash_250008_1", "business_type": "wash", "status": "approved", "tmdb_id": 250008, "media_title": "\\u6d4b\\u8bd5\\u7247", "media_year": 2026, "media_type": "tv", "season": 1, "wash_baseline": ["/library/old/a.mkv"]}
        x.update(overrides)
        return x

    def test_approved_is_eligible_and_body_is_bounded(self):
        x = self.item(media_title="A" * 500, wash_baseline=["/x/" + "b" * 800])
        self.assertTrue(bridge.eligible(x))
        body = bridge.body(x)
        self.assertIn('"request_id": "wash_250008_1"', body)
        self.assertIn('"baseline_path_count": 1', body)
        self.assertLessEqual(len(body), bridge.MAX_BODY)
        self.assertNotIn("TOKEN", body)
        self.assertNotIn("API_KEY", body)

    def test_non_approved_invalid_and_missing_baseline_are_skipped(self):
        for x in [self.item(status="pending"), self.item(status="completed"), self.item(business_type="request"), self.item(request_id="bad space"), self.item(tmdb_id=0), self.item(wash_baseline=[])]:
            self.assertFalse(bridge.eligible(x))

    def test_baseline_paths_are_capped(self):
        body = bridge.body(self.item(wash_baseline=[str(i) for i in range(100)]))
        self.assertIn('"baseline_path_count": 50', body)

    def test_idempotency_key_is_literal_request_id(self):
        x = self.item(request_id="wash:literal.id-1")
        args = bridge.dispatch_args(x)
        key_index = args.index("--idempotency-key") + 1
        self.assertEqual(args[key_index], "yimao-wash:wash:literal.id-1")

    def test_records_support_list_and_wrapped_json(self):
        x = self.item()
        self.assertEqual(len(bridge.records([x])), 1)
        self.assertEqual(len(bridge.records({"reviews": [x]})), 1)
        self.assertEqual(bridge.records({"bad": []}), [])

if __name__ == "__main__":
    unittest.main()
