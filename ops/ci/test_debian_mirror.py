import unittest

from debian_mirror import upstream_url


class DebianMirrorTests(unittest.TestCase):
    def test_only_the_expected_signed_archive_paths_are_forwarded(self):
        for path in ('/debian/dists/trixie/InRelease', '/debian/dists/trixie-updates/Release.gpg',
                     '/debian/pool/main/f/foo/foo_1+deb13u1_amd64.deb'):
            with self.subTest(path=path):
                self.assertEqual(upstream_url(path), 'https://deb.debian.org' + path)

    def test_cannot_use_the_test_mirror_as_an_open_proxy(self):
        for path in ('http://127.0.0.1/', '//deb.debian.org/debian/pool/x', '/debian/pool/../project',
                     '/debian/pool/%2e%2e/private', '/debian/dists/sid/InRelease', '/debian/pool/x?url=other',
                     '/debian/pool/x#fragment', '/debian/pool//x', '/debian/dists/trixie',
                     '/debian/pool/x%2fy', '/debian/pool/x%252bfoo.deb'):
            with self.subTest(path=path):
                with self.assertRaises(ValueError):
                    upstream_url(path)

    def test_can_use_a_listed_mirror_but_rejects_an_arbitrary_upstream(self):
        path = '/debian/dists/trixie/InRelease'
        self.assertEqual(upstream_url(path, 'ustc'), 'https://mirrors.ustc.edu.cn' + path)
        with self.assertRaises(ValueError):
            upstream_url(path, 'https://127.0.0.1')

    def test_apt_escaped_plus_is_accepted_in_debian_package_versions(self):
        path = '/debian/pool/main/b/busybox/busybox_1.37.0-6'
        for encoded in ('%2b', '%2B'):
            with self.subTest(encoded=encoded):
                self.assertEqual(upstream_url(path + encoded + 'b9_amd64.deb'),
                                 'https://deb.debian.org' + path + '+b9_amd64.deb')
