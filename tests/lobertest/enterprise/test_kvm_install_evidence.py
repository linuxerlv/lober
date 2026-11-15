import unittest

from kvm_install_evidence import check_intervals, check_shared_image, check_shared_tasks, check_terminal


class LowMemoryEvidenceTests(unittest.TestCase):
    def test_shared_install_container_cannot_change_image_or_instance(self):
        expected = "sha256:" + "1" * 64
        first = {"image_id": expected, "container_id": "2" * 64}
        check_shared_image(first, first, {"image_id": expected})
        for after in ({**first, "image_id": "sha256:" + "3" * 64},
                      {**first, "container_id": "3" * 64}, {}):
            with self.assertRaises(ValueError):
                check_shared_image(first, after, {"image_id": expected})

    def test_parallel_must_overlap_and_serial_must_not(self):
        check_intervals([(10, 25), (15, 30)], [(31, 45), (45, 60)])
        with self.assertRaises(ValueError):
            check_intervals([(10, 15), (15, 30)], [(31, 45), (45, 60)])
        with self.assertRaises(ValueError):
            check_intervals([(10, 25), (15, 30)], [(31, 45), (44, 60)])

    def test_repeat_cannot_add_events_or_reuse_the_wrong_serial(self):
        events = [{'seq': i, 'at': i + 10, 'taskID': 'task-a', 'kind': kind} for i, kind in enumerate(
            ('machine.installing', 'machine.init_start', 'machine.ready'), start=1)]
        first = {'task': 'task-a', 'serial': 'LOBER-KVM-PARALLEL-1', 'state': 'done', 'events': events}
        self.assertEqual(check_terminal(first, first, first['serial']), (11, 13))
        with self.assertRaises(ValueError):
            check_terminal(first, {**first, 'events': events[:-1]}, first['serial'])
        with self.assertRaises(ValueError):
            check_terminal(first, first, 'LOBER-KVM-PARALLEL-2')

    def test_one_candidate_instance_must_return_all_four_tasks(self):
        expected = {f'task-{n}': f'GUEST-{n}' for n in range(4)}
        tasks = [{'id': task, 'hostSN': serial, 'state': 'done'} for task, serial in expected.items()]
        check_shared_tasks(tasks, expected)
        with self.assertRaises(ValueError):
            check_shared_tasks(tasks[:2], expected)
        with self.assertRaises(ValueError):
            check_shared_tasks([*tasks, tasks[0]], expected)
