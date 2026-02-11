---types---
boolFalse#bc799737 = Bool;
boolTrue#997275b5 = Bool;
user#8f97c628 flags:# id:long first_name:flags.0?string last_name:flags.1?string = User;
userEmpty#d3bc4b7c = User;
sentCode#5e002502 type:string phone_code_hash:string = auth.SentCode;
authorization#33fb7bb8 user:User = auth.Authorization;

---functions---
auth.sendCode#a677244f phone_number:string api_id:int api_hash:string = auth.SentCode;
auth.signUp#80eee427 phone_number:string phone_code_hash:string first_name:string last_name:string = auth.Authorization;