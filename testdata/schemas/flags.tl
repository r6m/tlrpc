---types---
user#8f97c628 flags:# first_name:flags.0?string last_name:flags.0?string = User;
contact#12345678 flags:# name:string phone:flags.0?string email:flags.1?string = Contact;

---functions---
contacts.addContact#abcdef12 flags:# contact:Contact send_message:flags.0?true = Bool;