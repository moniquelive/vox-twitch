port module Main exposing (..)

import Browser
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Http
import Json.Decode as Json



-- MAIN


main : Program () Model Msg
main =
    Browser.element
        { init = init
        , update = update
        , subscriptions = subscriptions
        , view = view
        }



-- PORTS


port requestIdShare : () -> Cmd msg


port onAuthorized : (List String -> msg) -> Sub msg



-- MODEL


type alias Model =
    { textToSpeak : String
    , token : String
    , channelId : String
    , disableSubmit : Bool
    }


init : () -> ( Model, Cmd Msg )
init _ =
    ( Model "" "" "" True, Cmd.none )



-- UPDATE


type Msg
    = Submitted
    | TextChanged String
    | Posted (Result Http.Error ())
    | Authorized (List String)
    | KeyDown Int


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        KeyDown key ->
            if key == 13 then
                update Submitted model

            else
                ( model, Cmd.none )

        Authorized params ->
            case params of
                token :: channelId :: _ ->
                    ( { model | token = token, channelId = channelId }, Cmd.none )

                _ ->
                    ( model, Cmd.none )

        Posted _ ->
            ( { model | disableSubmit = False }, Cmd.none )

        Submitted ->
            let
                textToSpeak =
                    model.textToSpeak
            in
            ( { model | disableSubmit = True, textToSpeak = "" }
            , Cmd.batch
                [ requestIdShare ()
                , submitSpeech model.token textToSpeak
                ]
            )

        TextChanged text ->
            let
                disableSubmit =
                    String.length text == 0
            in
            ( { model | textToSpeak = text, disableSubmit = disableSubmit }
            , Cmd.none
            )


submitSpeech : String -> String -> Cmd Msg
submitSpeech token text =
    -- , url = "//localhost:7001/tts/"
    Http.request
        { method = "POST"
        , headers = [ Http.header "Authorization" ("Bearer " ++ token) ]
        , url = "https://vox-twitch.monique.dev/tts/"
        , body = Http.multipartBody [ Http.stringPart "text" text ]
        , expect = Http.expectWhatever Posted
        , timeout = Nothing
        , tracker = Nothing
        }



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions _ =
    Sub.batch [ onAuthorized Authorized ]



-- VIEW


onKeyDown : (Int -> msg) -> Attribute msg
onKeyDown tagger =
    on "keydown" (Json.map tagger keyCode)


view : Model -> Html Msg
view model =
    div [ class "card-body" ]
        [ header []
            [ img [ alt "Logo CyberVox", src "https://i1.wp.com/cybervox.ai/wp-content/uploads/sites/11/LOGO_footer_cybervox.png" ]
                []
            ]
        , article []
            [ h4 []
                [ text "Esta é a Pérola, voz mais maravilhosa de todas. Experimente!" ]
            , div [ id "form" ]
                [ div [ class "form-group" ]
                    [ label [ for "text" ]
                        [ text "Digite uma frase para a pérola falar:" ]
                    , input
                        [ class "input p-1 form-control"
                        , id "text"
                        , name "text"
                        , type_ "text"
                        , value model.textToSpeak
                        , onKeyDown KeyDown
                        , onInput TextChanged
                        ]
                        []
                    ]
                , input
                    [ class "btn btn-color"
                    , type_ "submit"
                    , value "Enviar"
                    , disabled model.disableSubmit
                    , onClick Submitted
                    ]
                    []
                ]
            ]
        , footer []
            [ span []
                [ strong []
                    [ text "Compartilhe: " ]
                ]
            , div [ class "form-group" ]
                [ a [ class "btn btn-color", href "https://twitch.tv/moniquelive", target "_blank" ]
                    [ i [ class "fab fa-twitch" ]
                        []
                    ]
                , a [ class "btn btn-color", href "https://twitter.com/cyberlabsai", target "_blank" ]
                    [ i [ class "fab fa-twitter" ]
                        []
                    ]
                , a [ class "btn btn-color", href "https://instagram.com/cyberlabsai", target "_blank" ]
                    [ i [ class "fab fa-instagram" ]
                        []
                    ]
                ]
            , div [ class "form-group" ]
                [ text "Powered by   "
                , a [ href "https://cybervox.ai", target "_blank" ]
                    [ text "CyberVox" ]
                , text "@"
                , a [ href "https://cyberlabs.ai", target "_blank" ]
                    [ text "CyberLabs" ]
                ]
            ]
        ]
