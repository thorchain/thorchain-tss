TSS Recovery
============

This tool is intended to generate the private key of a TSS pubkey, by
combining the secrets between each TSS member.

If you pass `export` and `password` it will generate a binance keystore file

```
tss-recovery -export <file path> -password <password> -n <num of participants
3 in a 3of4>
localstate-thorpub1addwnpepq22asyxl5fmq5klvsufrx56u78capnsgk84y0v8lqf0exjfgfldxqdhurgq.json
...
```
