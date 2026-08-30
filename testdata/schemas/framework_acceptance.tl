---types---
assetDocument#a1100001 flags:# id:long name:string summary:flags.0?string tags:Vector<string> = Asset;
assetLink#a1100002 flags:# id:long url:string title:flags.0?string tags:Vector<string> = Asset;
catalogPage#a1100003 flags:# items:Vector<Asset> next_cursor:flags.0?string = CatalogPage;
jobQueued#a1100004 job_id:long asset:Asset labels:Vector<string> = JobStatus;
jobRejected#a1100005 code:int reason:string = JobStatus;

---functions---
catalog.search#b2200001 flags:# query:string cursor:flags.0?string limit:int = CatalogPage;
catalog.resolve#b2200002 id:long = Asset;
workflow.submit#b2200003 flags:# asset:Asset labels:Vector<string> note:flags.0?string = JobStatus;
workflow.reject#b2200004 reason:string = JobStatus;
