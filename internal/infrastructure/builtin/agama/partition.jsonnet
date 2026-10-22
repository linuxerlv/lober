storage: {
  drives: [{
    search: '/dev/{{param:disk}}',
    alias: 'system',
    ptableType: 'gpt',
    partitions: [
      { search: '*', delete: true },
      { generate: 'default' },
    ],
  }],
  boot: { configure: true, device: 'system' },
},
