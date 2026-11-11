"""Scope checks only; these do not install an OS or connect to PVE."""
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

from wsl_install import WslCampaign


class WslCampaignScopes(unittest.TestCase):
    def test_pve_operations_are_always_forbidden(self):
        campaign=object.__new__(WslCampaign)
        with self.assertRaisesRegex(RuntimeError,'PVE'):
            campaign.pve('POST','/status/start')

    def test_unowned_state_directory_is_rejected_before_configuration(self):
        with TemporaryDirectory() as folder:
            with self.assertRaises(ValueError):WslCampaign(Path(folder),'debian')

    def test_ssh_uses_only_owned_forward_and_case_known_hosts(self):
        campaign=object.__new__(WslCampaign)
        campaign.root=Path('/owned-fixture')
        campaign.run=campaign.root/'real-debian-positive'
        args=campaign.guest_ssh_args('true')
        self.assertEqual(args[args.index('-p')+1],'22221')
        self.assertEqual(args[-2:],[ 'lober@127.0.0.1','true'])
        self.assertIn('UserKnownHostsFile=/owned-fixture/real-debian-positive/known_hosts',args)
        self.assertIn('IdentitiesOnly=yes',args)


if __name__=='__main__':unittest.main()
