port module Main exposing (..)

import Browser
import Html exposing (..)
import Html.Attributes exposing (..)
import Html.Events exposing (..)
import Json.Decode as D
import Time



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


port playUrl : String -> Cmd msg


port messageReceiver : (String -> msg) -> Sub msg



-- MODEL


type alias WebsocketMessage =
    { client_id : String
    , audio_url : String
    , text : String
    , username : String
    }


type alias Card =
    { username : String
    , text : String
    }


type alias Model =
    { cards : List Card
    }


init : () -> ( Model, Cmd Msg )
init _ =
    ( { cards = [] }, Cmd.none )



-- UPDATE


type Msg
    = Recv String
    | Tick Time.Posix


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        Tick _ ->
            ( { model | cards = List.tail model.cards |> Maybe.withDefault [] }
            , Cmd.none
            )

        Recv json ->
            case D.decodeString websocketMessageDecoder json of
                Ok ws ->
                    ( { model | cards = model.cards ++ [ Card ws.username ws.text ] }
                    , playUrl ws.audio_url
                    )

                Err _ ->
                    ( model, Cmd.none )



-- SUBSCRIPTIONS


subscriptions : Model -> Sub Msg
subscriptions _ =
    Sub.batch
        [ messageReceiver Recv
        , Time.every (60 * 1000) Tick
        ]



-- VIEW


cardView : Card -> Html Msg
cardView card =
    div []
        [ h3 [ class "username" ] [ text card.username ]
        , p [ class "text" ] [ text card.text ]
        ]


view : Model -> Html Msg
view model =
    node "marquee"
        [ attribute "scrolldelay" "60"
        , attribute "direction" "up"
        ]
        (List.map cardView model.cards)



-- JSON decode


websocketMessageDecoder : D.Decoder WebsocketMessage
websocketMessageDecoder =
    D.map4 WebsocketMessage
        (D.field "client_id" D.string)
        (D.field "audio_url" D.string)
        (D.field "text" D.string)
        (D.field "username" D.string)
