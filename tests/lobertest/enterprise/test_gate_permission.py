import unittest

from test_control import permission_case


class FakeRequest(unittest.TestCase):
    tokens = {"viewer": "test-only-token"}
    status = 422

    def request(self, *args):
        return self.status, {"code": self.status}

    def assert_status(self, response, expected):
        self.assertEqual(response[0], expected)


class PermissionAssertionTests(unittest.TestCase):
    def test_authorized_get_cannot_pass_with_validation_error(self):
        with self.assertRaises(AssertionError):
            permission_case("viewer", "GET", "/hosts", True)(FakeRequest())

    def test_deliberately_invalid_write_can_reach_validation(self):
        request = FakeRequest()
        request.status = 400
        permission_case("viewer", "POST", "/hosts", True)(request)
