---types---
user1#12345678 id:long = User;
user2#12345678 name:string = User;
invalidUser#999999 name:UndefinedType = User;

---functions---
auth.sendCode1#abcdef12 = auth.SentCode;
auth.sendCode2#abcdef12 = auth.SentCode;