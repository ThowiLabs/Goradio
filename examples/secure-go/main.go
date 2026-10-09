package main

import(
 "context"
 "log"
 "os"
 "os/signal"
 "path/filepath"
 "goradio"
)
func main(){
 app,err:=goradio.New(goradio.Config{
  Title:"Goradio Go — Usuarios y Persistencia",
  Inputs:[]goradio.Component{
   {Type:"textbox",Label:"Nombre",Required:true},
   {Type:"textbox",Label:"Texto persistente"},
  },
  Outputs:[]goradio.Component{{Type:"textbox",Label:"Resultado"}},
 },func(_ context.Context,v []any)([]any,error){
  return []any{"¡Hola "+v[0].(string)+"! "+v[1].(string)},nil
 },"")
 if err!=nil{log.Fatal(err)}
 defer app.Close()
 home,err:=os.UserHomeDir();if err!=nil{log.Fatal(err)}
 err=app.EnableSecurity(goradio.SecurityConfig{
  DBPath:filepath.Join(home,".goradio","secure-go.sqlite"),
  AppID:"secure-go",RequireLogin:true,AllowSignup:false,
  PersistInputs:true,PersistOutputs:true,
 })
 if err!=nil{log.Fatal(err)}
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt);defer stop()
 exists,err:=app.UserExists(ctx,"admin");if err!=nil{log.Fatal(err)}
 if !exists{
  pwd:=os.Getenv("GORADIO_ADMIN_PASSWORD")
  if pwd==""{log.Fatal("Define GORADIO_ADMIN_PASSWORD (4+ caracteres) en el primer inicio")}
  if err:=app.CreateUser(ctx,"admin",pwd,"admin");err!=nil{log.Fatal(err)}
 }
 if err:=app.Serve(ctx,"127.0.0.1",7917,nil);err!=nil{log.Fatal(err)}
}
